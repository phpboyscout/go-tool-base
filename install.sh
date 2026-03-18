#!/usr/bin/env bash

set -eo pipefail

GTB_NON_INTERACTIVE=${GTB_NON_INTERACTIVE:-false}

if [ "$GTB_NON_INTERACTIVE" = true ]; then
  echo "INFO: GTB_NON_INTERACTIVE is set to true. Running in non-interactive mode."
else
  echo "INFO: GTB_NON_INTERACTIVE is not set. Running in interactive mode."
fi

# Function to handle cleanup
cleanup() {
  echo "Cleaning up..."
  if [ -n "${package_name_to_clean}" ] && [ -f "${package_name_to_clean}" ]; then
    rm -f -- "${package_name_to_clean}"
  fi
}

# Trap to call cleanup function on script exit or interruption
trap cleanup EXIT HUP INT QUIT TERM

# Check if GITHUB_TOKEN is set
if [ -z "${GITHUB_TOKEN}" ]; then
  echo "Error: The GITHUB_TOKEN environment variable is not set."
  echo "Please set it to a token with access to the repository releases."
  exit 1
fi

# Check for required tools
for tool in curl jq uname tar mkdir mv; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "Error: Required command '${tool}' is not installed or not in PATH."
    exit 1
  fi
done

local_bin_dir="$HOME/.local/bin"
executable_path="${local_bin_dir}/gtb"

# Check if gtb is already installed
if [ -x "${executable_path}" ]; then
  echo "INFO: 'gtb' binary is already installed at ${executable_path}."
  echo "To update, you can try running 'gtb update'."
  echo ""
  if [ "$GTB_NON_INTERACTIVE" = false ]; then
    printf "Do you want to proceed with re-installing gtb? (y/N): "
    read -r reinstall_choice
  else
    reinstall_choice="N"
  fi
  case "$reinstall_choice" in
    [yY][eE][sS]|[yY])
      echo "Proceeding with re-installation..."
      ;;
    *)
      echo "Re-installation cancelled by user."
      exit 0
      ;;
  esac
fi

echo "Determining package for your system..."
# Determine the package name based on the system's OS and architecture
arch=$(uname -m)
if [ "$arch" == "aarch64" ]; then
  arch="arm64"
fi
package_name="gtb_$(uname -s)_${arch}.tar.gz"
package_name_to_clean="${package_name}" # Variable for trap cleanup

echo "Fetching latest release information from github.com..."
api_url="https://api.github.com/repos/phpboyscout/gtb/releases/latest"
download_url=$(curl -sL -H "Authorization: token ${GITHUB_TOKEN}" -H "Accept: application/vnd.github.v3+json" "${api_url}" | jq -r ".assets[] | select(.name == \"${package_name}\") | .browser_download_url")


if [[ -z "${download_url}" || "${download_url}" == "null" ]]; then
  echo "Error: Could not find download URL for package '${package_name}'."
  echo "Please check if a release asset matching your OS and architecture exists."
  exit 1
fi

echo "Download URL: $download_url"
echo "Downloading ${package_name}..."

# Download the package
curl -fL -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/octet-stream" -o "${package_name}" "${download_url}"
if [ $? -ne 0 ]; then
  echo "Error: Failed to download '${package_name}' from '${download_url}'."
  exit 1
fi

echo "Extracting gtb binary..."
# Extract the gtb binary
tar -xzf "${package_name}" gtb
if [ $? -ne 0 ]; then
  echo "Error: Failed to extract 'gtb' from '${package_name}'."
  exit 1
fi

# Create ~/.local/bin if it doesn't exist
echo "Ensuring ~/.local/bin directory exists..."
mkdir -p "$local_bin_dir"

# Move the binary to ~/.local/bin/
echo "Installing gtb to $local_bin_dir..."
mv -f "gtb" "$local_bin_dir"

# Check if ~/.local/bin is in PATH and print instructions if not

case ":$PATH:" in
  *":${local_bin_dir}:"*)
    # In PATH, do nothing
    ;;
  *)
    echo "" # Add a newline for better readability
    echo "--------------------------------------------------------------------------------"
    echo "WARNING: ${local_bin_dir} is not in your \$PATH."
    echo "The 'gtb' binary has been installed to ${local_bin_dir}."
    echo ""
    echo "To run 'gtb' directly, you need to add ${local_bin_dir} to your \$PATH."
    echo "You can typically do this by adding the following line to your shell's"
    echo "configuration file (e.g., ~/.bashrc, ~/.zshrc, ~/.profile):"
    echo ""
    echo "  export PATH=\"${local_bin_dir}:\$PATH\""
    echo ""
    echo "After adding the line, please open a new terminal session or source your"
    echo "shell configuration file (e.g., 'source ~/.bashrc')."
    echo "--------------------------------------------------------------------------------"
    ;;
esac

# Cleanup is handled by the trap
echo "gtb binary installed successfully!"
