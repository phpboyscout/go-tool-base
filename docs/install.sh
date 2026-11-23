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

# gtb releases are published to GitLab (gitlab.com/phpboyscout/go-tool-base).
# GITLAB_TOKEN is OPTIONAL for installing from this public project. When
# absent, we fetch anonymously. When present (handy behind a private mirror,
# or to avoid anonymous rate limits), it is sent as a PRIVATE-TOKEN header.
AUTH_HEADER=""
if [ -n "${GITLAB_TOKEN:-}" ]; then
  AUTH_HEADER="PRIVATE-TOKEN: ${GITLAB_TOKEN}"
  echo "INFO: Using GITLAB_TOKEN for authenticated requests."
else
  echo "INFO: No GITLAB_TOKEN set. Proceeding anonymously."
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

echo "Fetching latest release information from gitlab.com..."
# permalink/latest 302-redirects to the newest tagged release; -L follows it.
project_path="phpboyscout%2Fgo-tool-base"
api_url="https://gitlab.com/api/v4/projects/${project_path}/releases/permalink/latest"

# The status goes on a line of its own after the body, so an error page is
# reported as one rather than handed to jq.
curl_args=(-sSL -w '\n%{http_code}')
if [ -n "${AUTH_HEADER}" ]; then
  curl_args+=(-H "${AUTH_HEADER}")
fi

if ! response=$(curl "${curl_args[@]}" "${api_url}"); then
  echo "Error: Could not reach ${api_url}."
  exit 1
fi

status=${response##*$'\n'}
release_json=${response%$'\n'*}

if [ "${status}" != "200" ]; then
  echo "Error: GitLab answered HTTP ${status} when asked for the latest release:"
  # Stripped to printable text, since the body is the server's and reaches the
  # terminal. sed reads it all, so a long body cannot SIGPIPE the pipeline.
  printf '%s\n' "${release_json}" | LC_ALL=C tr -d '\000-\010\013-\037\177-\377' | cut -c 1-200 | sed -n '1,3s/^/  /p'
  case "${status}" in
    429)
      echo "GitLab limits anonymous API calls per IP address. Retry in a few minutes,"
      echo "or set GITLAB_TOKEN to a GitLab personal access token with the read_api scope."
      ;;
    401|403)
      echo "Check that GITLAB_TOKEN is a valid token with the read_api scope, or unset it."
      ;;
  esac
  exit 1
fi

if [ -z "${release_json//[[:space:]]/}" ]; then
  echo "Error: The latest release information from GitLab was empty, not JSON."
  exit 1
fi

# Each release asset is an entry under .assets.links[]; match ours by name and
# take its direct_asset_url (a stable …/-/releases/<tag>/downloads/<name> route).
if ! download_url=$(printf '%s' "${release_json}" \
  | jq -r --arg name "${package_name}" \
       '.assets.links[]? | select(.name == $name) | .direct_asset_url' 2>/dev/null); then
  echo "Error: The latest release information from GitLab was not JSON."
  exit 1
fi

if [[ -z "${download_url}" || "${download_url}" == "null" ]]; then
  echo "Error: Could not find a release asset named '${package_name}'."
  echo "Please check that a release asset matching your OS and architecture exists."
  exit 1
fi

echo "Downloading ${package_name}..."
if [ -n "${AUTH_HEADER}" ]; then
  curl -fL -H "${AUTH_HEADER}" -o "${package_name}" "${download_url}"
else
  curl -fL -o "${package_name}" "${download_url}"
fi
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
