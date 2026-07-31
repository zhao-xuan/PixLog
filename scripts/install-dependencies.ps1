$ErrorActionPreference = "Stop"

if (Get-Command chafa -ErrorAction SilentlyContinue) {
    chafa --version
    exit 0
}

if (Get-Command winget -ErrorAction SilentlyContinue) {
    winget install --exact --id hpjansson.Chafa --accept-package-agreements --accept-source-agreements
} elseif (Get-Command scoop -ErrorAction SilentlyContinue) {
    scoop install chafa
} else {
    throw "No supported package manager found. Install Chafa from https://hpjansson.org/chafa/download/"
}

if (-not (Get-Command chafa -ErrorAction SilentlyContinue)) {
    throw "Chafa was installed but is not available on PATH. Open a new terminal and try again."
}
chafa --version