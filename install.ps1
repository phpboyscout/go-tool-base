#Requires -Version 5.1

<#
.SYNOPSIS
    Installs or updates the 'gtb' utility.
.DESCRIPTION
    This script downloads the appropriate 'gtb' binary for your system
    from GitHub releases, extracts it, and installs it to a local binary
    directory. It also checks if this directory is in your PATH.
.NOTES
    Author: Matt Cockayne <matt@phpboyscout.com>
    Version: 1.0
    Requires GITHUB_TOKEN environment variable to be set for accessing releases.
#>

# Strict mode and error handling
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop" # Exit on terminating errors

# Global variable for the temporary directory for robust cleanup
$script:tempDir = $null

try {
    # --- Configuration ---
    $repoOwner = "phpboyscout"
    $repoName = "gtb"
    $gitApiBaseUrl = "https://api.github.com" # Adjust if different from public GitHub

    # --- 1. GITHUB_TOKEN Check (optional) ---
    # M-8 from docs/development/reports/security-audit-2026-04-17.md —
    # GITHUB_TOKEN is OPTIONAL for public repositories. When absent, the
    # script falls back to anonymous access, subject to GitHub's 60 req/hr
    # rate limit (unlikely to trip on a single install). When present and
    # broadly scoped, warn the user.
    $authHeader = $null
    if (-not [string]::IsNullOrEmpty($env:GITHUB_TOKEN)) {
        $authHeader = @{ Authorization = "token $($env:GITHUB_TOKEN)" }

        try {
            $scopeResp = Invoke-WebRequest -Method Head -Uri "$gitApiBaseUrl/user" -Headers $authHeader -UseBasicParsing -ErrorAction Stop
            $scopes = $scopeResp.Headers["X-OAuth-Scopes"]
            if ($scopes -and ($scopes -match '(^|,\s*)(repo|admin:|delete_repo|workflow)($|,)')) {
                Write-Warning "GITHUB_TOKEN appears to have broad scopes ($scopes)."
                Write-Warning "For installing releases from a public repo, a fine-grained"
                Write-Warning "token with only 'contents: read' on this repo is sufficient."
                Write-Warning "Proceeding anyway..."
            }
        } catch {
            # Scope check failure is non-fatal — token may still work.
        }
    } else {
        Write-Host "INFO: No GITHUB_TOKEN set. Proceeding anonymously (subject to rate limits)."
    }

    # --- 2. Prerequisite PowerShell Cmdlet Check ---
    Write-Host "Checking prerequisites..."
    $requiredCmdlets = @("Invoke-RestMethod", "Invoke-WebRequest", "Get-Command", "Test-Path", "New-Item", "Move-Item", "Remove-Item", "Join-Path") # Removed Expand-Archive
    foreach ($cmdletName in $requiredCmdlets) {
        if (-not (Get-Command $cmdletName -ErrorAction SilentlyContinue)) {
            Write-Error "Error: Required PowerShell cmdlet '${cmdletName}' is not available."
            Write-Error "Please ensure you are using PowerShell 5.1 or newer."
            exit 1
        }
    }

    # Check for external 'tar' command
    if (-not (Get-Command tar -ErrorAction SilentlyContinue)) {
        Write-Error "Error: The 'tar' command-line utility is not available or not in PATH."
        Write-Error "This script requires 'tar' to extract .tar.gz archives."
        Write-Error "On Windows, tar is typically included with recent versions or can be installed with Git for Windows or WSL."
        exit 1
    }

    Write-Host "Prerequisites met."

    # --- 3. Determine OS and Architecture (Windows Only) ---
    Write-Host "Assuming Windows OS. Determining architecture..."
    $osIdentifier = "Windows" # Hardcoded for Windows-only script
    $archIdentifier = ""

    # Determine native OS architecture using environment variables for broader PS 5.1 compatibility
    $nativeOsArch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        # If PROCESSOR_ARCHITEW6432 is set, PowerShell is running as a 32-bit process
        # on a 64-bit OS. This variable holds the native 64-bit architecture of the OS.
        $nativeOsArch = $env:PROCESSOR_ARCHITEW6432
    }

    switch ($nativeOsArch) {
        "AMD64" { $archIdentifier = "x86_64" } # Standard identifier for 64-bit x86
        "ARM64" { $archIdentifier = "arm64" }
        default {
            # This will also catch "x86" if it's a 32-bit OS, which is unsupported by current asset list
            Write-Error "Unsupported Windows architecture: '$nativeOsArch'. This script currently supports downloading binaries for AMD64 (x86_64) and ARM64 architectures."
            exit 1
        }
    }
    Write-Host "Detected OS: $osIdentifier, Architecture: $archIdentifier"

    # --- 4. Define local_bin_dir and executable_path ---
    # Use a Windows-conventional path: %LOCALAPPDATA%\Programs\gtb, scoped to the current user
    $localBinDir = Join-Path $env:LOCALAPPDATA "Programs\gtb"
    $executableName = "gtb.exe" # Hardcoded for Windows
    $executablePath = Join-Path $localBinDir $executableName

    # --- 5. Check if gtb is already installed ---
    if (Test-Path -Path $executablePath -PathType Leaf) {
        Write-Host "INFO: '$executableName' binary is already installed at '$executablePath'." -ForegroundColor Cyan
        Write-Host "To update, you can try running '$executableName update'." -ForegroundColor Cyan
        Write-Host ""
        $reinstallChoice = Read-Host "Do you want to proceed with re-installing the gtb tool? (y/N)"
        if ($reinstallChoice -notmatch '^[yY]([eE][sS])?$') {
            Write-Host "Re-installation cancelled by user."
            exit 0
        }
        Write-Host "Proceeding with re-installation..."
    }

    # --- Create Temporary Directory ---
    # Ensure tempDir is script-scoped for the finally block
    $script:tempDir = New-Item -ItemType Directory -Path (Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName()))
    Write-Host "Temporary directory created: $($script:tempDir.FullName)"

    # --- 6. Fetch Latest Release Information from API ---
    Write-Host "Fetching latest release information from $gitApiBaseUrl for $repoOwner/$repoName..."
    $latestReleaseApiUrl = "$gitApiBaseUrl/repos/$repoOwner/$repoName/releases/latest"
    $apiHeaders = @{
        "Accept" = "application/vnd.github.v3+json"
    }
    if ($authHeader) {
        $apiHeaders["Authorization"] = $authHeader["Authorization"]
    }

    try {
        $releaseInfo = Invoke-RestMethod -Uri $latestReleaseApiUrl -Headers $apiHeaders -Method Get
    }
    catch {
        Write-Error "Error fetching release information from $latestReleaseApiUrl."
        if ($_.Exception.Response) {
            $statusCode = $_.Exception.Response.StatusCode
            Write-Error "Status: $statusCode. Message: $($_.Exception.Message)"
            $errorResponseStream = $_.Exception.Response.GetResponseStream()
            $streamReader = New-Object System.IO.StreamReader($errorResponseStream)
            $errorBody = $streamReader.ReadToEnd()
            $streamReader.Close()
            $errorResponseStream.Close()
            Write-Error "Response body: $errorBody"
        } else {
            Write-Error "Message: $($_.Exception.Message)"
        }
        exit 1
    }

    # Determine the asset to download
    $downloadAsset = $null
    $packageName = ""
    $isDirectExecutableDownload = $false

    # For Windows, prioritize a direct .exe if available
    $windowsExeAssetNames = @(
        "gtb_${osIdentifier}_${archIdentifier}.exe", # e.g., gtb_Windows_x86_64.exe
        "gtb.exe"                                   # Generic gtb.exe
    )
    foreach ($assetName in $windowsExeAssetNames) {
        $downloadAsset = $releaseInfo.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
        if ($null -ne $downloadAsset) {
            $packageName = $downloadAsset.name
            $isDirectExecutableDownload = $true
            Write-Host "Found direct Windows executable asset: $packageName"
            break
        }
    }

    # If not a direct Windows exe, look for the .tar.gz
    if ($null -eq $downloadAsset) {
        $tarGzPackageName = "gtb_${osIdentifier}_${archIdentifier}.tar.gz"
        $downloadAsset = $releaseInfo.assets | Where-Object { $_.name -eq $tarGzPackageName } | Select-Object -First 1
        if ($null -ne $downloadAsset) {
            $packageName = $downloadAsset.name
            Write-Host "Found tar.gz package asset for Windows: $packageName"
        }
    }

    if ($null -eq $downloadAsset) {
        Write-Error "Error: Could not find a suitable download asset for ${osIdentifier}/${archIdentifier}."
        Write-Error "Looked for 'gtb_${osIdentifier}_${archIdentifier}.tar.gz' and potential direct executables for Windows."
        Write-Host "Available assets from release:"
        $releaseInfo.assets | ForEach-Object { Write-Host "- $($_.name)" }
        exit 1
    }

    $downloadUrl = $downloadAsset.url
    Write-Host "Selected package for download: $packageName"
    Write-Host "Download URL: $downloadUrl"

    # --- 7. Download the Package ---
    $downloadPath = Join-Path $script:tempDir.FullName $packageName
    Write-Host "Downloading $packageName to $downloadPath..."
    $downloadHeaders = @{
        "Accept" = "application/octet-stream"
    }
    if ($authHeader) {
        $downloadHeaders["Authorization"] = "Bearer $($env:GITHUB_TOKEN)"
    }
    try {
        Invoke-WebRequest -Uri $downloadUrl -Headers $downloadHeaders -OutFile $downloadPath -UseBasicParsing
    }
    catch {
        Write-Error "Error: Failed to download '$packageName' from '$downloadUrl'."
        Write-Error "Status: $($_.Exception.Response.StatusCode). Message: $($_.Exception.Message)"
        exit 1
    }
    Write-Host "Download complete."

    # --- 8. Prepare Binary (Extract if .tar.gz, or use directly if .exe) ---
    Write-Host "Preparing '$executableName' binary..."
    $sourceBinaryPathInTemp = ""

    if ($isDirectExecutableDownload) {
        Write-Host "$packageName is a direct executable. No extraction needed."
        $sourceBinaryPathInTemp = $downloadPath
        # Ensure the downloaded file is named as expected ($executableName) in the temp dir
        if ((Get-Item $sourceBinaryPathInTemp).Name -ne $executableName) {
            $renamedPath = Join-Path $script:tempDir.FullName $executableName
            Move-Item -Path $sourceBinaryPathInTemp -Destination $renamedPath -Force
            $sourceBinaryPathInTemp = $renamedPath
        }
    }
    else { # It's a .tar.gz archive
        Write-Host "Extracting '$executableName' from '$packageName' using tar..."
        $extractTargetDir = Join-Path $script:tempDir.FullName "extracted_go_tool_base"
        New-Item -ItemType Directory -Path $extractTargetDir -Force | Out-Null

        try {
            # Use tar to extract the archive
            $tarArguments = @("-xzf", $downloadPath, "-C", $extractTargetDir)
            Write-Host "Executing: tar $($tarArguments -join ' ')"
            & tar $tarArguments

            if ($LASTEXITCODE -ne 0) {
                throw "tar command failed with exit code $LASTEXITCODE. Download path: '$downloadPath', Extraction dir: '$extractTargetDir'."
            }

            $expectedBinaryInArchive = Join-Path $extractTargetDir "gtb.exe"

            if (-not (Test-Path $expectedBinaryInArchive -PathType Leaf)) {
                Write-Error "Error: Could not find 'gtb.exe' in the extracted archive at '$extractTargetDir'."
                Write-Host "Contents of extracted archive:"
                Get-ChildItem -Path $extractTargetDir -Recurse | ForEach-Object { Write-Host "- $($_.FullName)" }
                exit 1
            }
            # Move the target binary to the root of the temp directory for easier handling
            $sourceBinaryPathInTemp = Join-Path $script:tempDir.FullName $executableName
            Move-Item -Path $expectedBinaryInArchive -Destination $sourceBinaryPathInTemp -Force
            Write-Host "Extraction successful using tar. '$executableName' is ready."
        }
        catch {
            $originalCmdletErrorRecord = $_
            $failedCommandMessage = $originalCmdletErrorRecord.Exception.Message
            $customMessage = "Error: Failed to extract '$executableName' from '$packageName' using tar."
            Write-Error "$customMessage`nUnderlying error from PowerShell: $failedCommandMessage"
            exit 1
        }
    }

    # --- 9. Create installation directory if it doesn't exist ---
    Write-Host "Ensuring installation directory '$localBinDir' exists..."
    if (-not (Test-Path -Path $localBinDir -PathType Container)) {
        New-Item -ItemType Directory -Path $localBinDir -Force | Out-Null
        Write-Host "Created directory: $localBinDir"
    }

    # --- 10. Move the Binary to the Installation Directory ---
    Write-Host "Installing '$executableName' to '$localBinDir'..."
    Move-Item -Path $sourceBinaryPathInTemp -Destination $executablePath -Force
    Write-Host "'$executableName' installed to '$executablePath'."

    # --- 11. Check if installation directory is in PATH and print instructions if not ---
    Write-Host "Checking if '$localBinDir' is in your system PATH..."
    $currentPathEnv = $env:PATH
    $pathSeparator = [System.IO.Path]::PathSeparator
    $pathArray = $currentPathEnv -split $pathSeparator

    $isInPath = $pathArray | Where-Object { $_ -eq $localBinDir } | Select-Object -First 1

    if (-not $isInPath) {
        Write-Host ""
        Write-Host "--------------------------------------------------------------------------------" -ForegroundColor Yellow
        Write-Warning "$localBinDir is not in your PATH."
        Write-Host "Attempting to add '$localBinDir' to your User PATH environment variable permanently..."

        try {
            $currentUserPath = [System.Environment]::GetEnvironmentVariable('PATH', 'User')
            if ([string]::IsNullOrEmpty($currentUserPath)) {
                $newPath = $localBinDir
            } else {
                $newPath = "$localBinDir$pathSeparator$currentUserPath"
            }
            [System.Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
            Write-Host "'$localBinDir' has been successfully added to your User PATH." -ForegroundColor Green
            Write-Host ""
            Write-Host "You will need to open a new PowerShell session or restart your terminal for this change to take effect." -ForegroundColor Yellow
        } catch {
            Write-Warning "Failed to automatically update the User PATH. Error: $($_.Exception.Message)"
            Write-Warning "Please add '$localBinDir' to your PATH manually using the following command."
            Write-Warning ""
            Write-Warning "  [System.Environment]::SetEnvironmentVariable('PATH', \"$newPath\", 'User') "
        }
        Write-Host "--------------------------------------------------------------------------------" -ForegroundColor Yellow
    } else {
        Write-Host "'$localBinDir' is already in your PATH." -ForegroundColor Green
    }

    Write-Host ""
    Write-Host "'$executableName' binary installed successfully!" -ForegroundColor Green
}
catch {
    Write-Error "An unexpected error occurred during installation: $($_.Exception.Message)"
    exit 1
}
finally {
    # Cleanup the temporary directory
    if (-not [string]::IsNullOrEmpty($script:tempDir) -and (Test-Path $script:tempDir.FullName -PathType Container)) {
        Write-Host "Cleaning up temporary directory: $($script:tempDir.FullName)..."
        Remove-Item -Recurse -Force -Path $script:tempDir.FullName -ErrorAction SilentlyContinue
    }
    Write-Host "Script finished."
}
