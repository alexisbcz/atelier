#!/bin/sh
# Sets up local image generation for atelier and starts the server:
# stable-diffusion.cpp (built with Metal on macOS), the SD-Turbo model and
# the TAESD decoder. An image takes about a second on an Apple M-series
# laptop after the first one.
#
#   scripts/local-ai.sh [dir]          # default dir: ~/.cache/atelier-ai
#   go run ./cmd/demo -sd http://127.0.0.1:7860
#
# Licenses: stable-diffusion.cpp and TAESD are MIT. SD-Turbo is under the
# Stability AI Non-Commercial Research Community License: for commercial
# use, pick another model (sd-server takes any Stable Diffusion or FLUX
# model) and check its license.
set -eu

dir=${1:-"$HOME/.cache/atelier-ai"}
port=${ATELIER_SD_PORT:-7860}
mkdir -p "$dir"
cd "$dir"

if [ ! -x sdcpp/build/bin/sd-server ]; then
  [ -d sdcpp ] || git clone --recursive --depth 1 https://github.com/leejet/stable-diffusion.cpp sdcpp
  backend=""
  if [ "$(uname -s)" = "Darwin" ]; then backend="-DSD_METAL=ON"; fi
  cmake -S sdcpp -B sdcpp/build $backend -DCMAKE_BUILD_TYPE=Release -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++
  cmake --build sdcpp/build --config Release -j
fi

fetch() {
  if [ ! -s "$2" ]; then
    curl -fL --progress-bar -o "$2.part" "$1"
    mv -f "$2.part" "$2"
  fi
}
fetch https://huggingface.co/stabilityai/sd-turbo/resolve/main/sd_turbo.safetensors sd_turbo.safetensors
fetch https://huggingface.co/madebyollin/taesd/resolve/main/diffusion_pytorch_model.safetensors taesd.safetensors

echo "sd-server on http://127.0.0.1:$port"
exec sdcpp/build/bin/sd-server -m sd_turbo.safetensors --taesd taesd.safetensors --listen-port "$port"
