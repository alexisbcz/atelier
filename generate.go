package atelier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ImageRequest asks for an image from a text prompt.
type ImageRequest struct {
	Prompt   string `json:"prompt"`
	Negative string `json:"negative,omitempty"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	// Steps and CFG suit the model: 1 step and CFG 1 for turbo models.
	Steps int     `json:"steps,omitempty"`
	CFG   float64 `json:"cfg,omitempty"`
	// Seed makes results reproducible (-1: random).
	Seed int64 `json:"seed,omitempty"`
}

// ImageGenerator turns a prompt into an image (PNG or JPEG bytes).
type ImageGenerator interface {
	Generate(ctx context.Context, req ImageRequest) ([]byte, error)
}

// SDServer talks to a stable-diffusion.cpp server (sd-server), which keeps
// the model loaded: after the first request, an image costs only its
// sampling. It also works with any server answering the OpenAI images API
// with b64_json responses.
type SDServer struct {
	// URL is the server's base URL ("http://127.0.0.1:7860").
	URL string
	// Steps and CFG are the defaults for requests that set none.
	Steps int
	CFG   float64
	// Client is the HTTP client (http.DefaultClient when nil).
	Client *http.Client
}

// Generate implements ImageGenerator.
func (s SDServer) Generate(ctx context.Context, req ImageRequest) ([]byte, error) {
	req = withRequestDefaults(req, s.Steps, s.CFG)
	// sd-server takes the generation parameters in an <sd_cpp_extra_args>
	// block of the prompt; the OpenAI fields carry the prompt and size.
	extra, _ := json.Marshal(map[string]any{
		"negative_prompt": req.Negative,
		"seed":            req.Seed,
		"sample_params": map[string]any{
			"sample_steps": req.Steps,
			"guidance":     map[string]any{"txt_cfg": req.CFG},
		},
	})
	body, _ := json.Marshal(map[string]any{
		"prompt":        req.Prompt + "<sd_cpp_extra_args>" + string(extra) + "</sd_cpp_extra_args>",
		"n":             1,
		"size":          strconv.Itoa(req.Width) + "x" + strconv.Itoa(req.Height),
		"output_format": "png",
	})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL+"/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("atelier: image server: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("atelier: image server: %s: %.200s", res.Status, b)
	}
	var out struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil || len(out.Data) == 0 {
		return nil, fmt.Errorf("atelier: image server: unexpected response %.200s", b)
	}
	return base64.StdEncoding.DecodeString(out.Data[0].B64JSON)
}

// SDCommand runs the stable-diffusion.cpp command (sd-cli) for each image.
// Simple, but the model loads for every image: prefer SDServer.
type SDCommand struct {
	Binary string   // path to sd-cli
	Model  string   // path to the model file
	Args   []string // extra arguments ("--diffusion-fa", ...)
	Steps  int
	CFG    float64

	mu sync.Mutex // one generation at a time: they share the GPU
}

// Generate implements ImageGenerator.
func (s *SDCommand) Generate(ctx context.Context, req ImageRequest) ([]byte, error) {
	req = withRequestDefaults(req, s.Steps, s.CFG)
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := os.MkdirTemp("", "atelier-gen")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.png")
	args := append([]string{
		"-m", s.Model, "-p", req.Prompt, "-n", req.Negative,
		"-W", strconv.Itoa(req.Width), "-H", strconv.Itoa(req.Height),
		"--steps", strconv.Itoa(req.Steps), "--cfg-scale", strconv.FormatFloat(req.CFG, 'f', -1, 64),
		"-s", strconv.FormatInt(req.Seed, 10), "-o", out,
	}, s.Args...)
	cmd := exec.CommandContext(ctx, s.Binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("atelier: %s: %w: %.300s", filepath.Base(s.Binary), err, stderr.String())
	}
	return os.ReadFile(out)
}

func withRequestDefaults(req ImageRequest, steps int, cfg float64) ImageRequest {
	if req.Width <= 0 {
		req.Width = 512
	}
	if req.Height <= 0 {
		req.Height = 512
	}
	if req.Steps <= 0 {
		req.Steps = steps
	}
	if req.Steps <= 0 {
		req.Steps = 1
	}
	if req.CFG <= 0 {
		req.CFG = cfg
	}
	if req.CFG <= 0 {
		req.CFG = 1
	}
	if req.Seed == 0 {
		req.Seed = -1
	}
	return req
}

// validateImageRequest bounds what clients may ask for.
func validateImageRequest(req ImageRequest) error {
	switch {
	case req.Prompt == "" || len(req.Prompt) > 2000:
		return errors.New("prompt must have 1 to 2000 characters")
	case len(req.Negative) > 2000:
		return errors.New("negative prompt too long")
	case req.Width < 0 || req.Height < 0 || req.Width > 2048 || req.Height > 2048 || req.Width%64 != 0 || req.Height%64 != 0:
		return errors.New("width and height must be multiples of 64, at most 2048")
	case req.Steps < 0 || req.Steps > 100:
		return errors.New("steps must be at most 100")
	case req.CFG < 0 || req.CFG > 30:
		return errors.New("cfg must be at most 30")
	}
	return nil
}

// generationTimeout bounds one generation.
const generationTimeout = 5 * time.Minute
