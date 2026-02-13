#!/usr/bin/env bash
set -e

# ============================================================
# MClaw Setup Script
# Installs optional dependencies for full functionality
# ============================================================

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
CHECK="${GREEN}✓${NC}"
WARN="${YELLOW}⚠${NC}"
CROSS="${RED}✗${NC}"

echo ""
echo -e "${CYAN}🦞 MClaw Setup${NC}"
echo "============================================"
echo ""

# Detect OS
OS="unknown"
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    if command -v apt-get &>/dev/null; then
        OS="debian"
    elif command -v yum &>/dev/null; then
        OS="redhat"
    elif command -v apk &>/dev/null; then
        OS="alpine"
    elif command -v pkg &>/dev/null; then
        OS="termux"
    fi
elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
fi

echo -e "Detected OS: ${CYAN}${OS}${NC}"
echo ""

# ---- 1. Check MClaw binary ----
echo "1. Checking MClaw binary..."
if command -v ./mclaw &>/dev/null || [ -f "./mclaw" ]; then
    echo -e "   ${CHECK} MClaw binary found"
else
    echo -e "   ${WARN} MClaw binary not found in current directory"
    echo "   Download from: https://github.com/ntminh611/mClaw/releases"
fi
echo ""

# ---- 2. Check/Install Chrome/Chromium (optional) ----
echo "2. Checking Chrome/Chromium (optional — for browser tool)..."
CHROME_FOUND=false
for cmd in google-chrome google-chrome-stable chromium chromium-browser; do
    if command -v "$cmd" &>/dev/null; then
        CHROME_FOUND=true
        echo -e "   ${CHECK} Found: $(command -v $cmd)"
        break
    fi
done

if [[ "$OS" == "macos" ]]; then
    if [ -d "/Applications/Google Chrome.app" ]; then
        CHROME_FOUND=true
        echo -e "   ${CHECK} Found: Google Chrome.app"
    fi
fi

if [ "$CHROME_FOUND" = false ]; then
    echo -e "   ${WARN} Chrome/Chromium not found"
    echo ""
    read -p "   Install Chromium? (y/N): " install_chrome
    if [[ "$install_chrome" =~ ^[Yy]$ ]]; then
        case "$OS" in
            debian)
                sudo apt-get update && sudo apt-get install -y chromium-browser || sudo apt-get install -y chromium
                ;;
            redhat)
                sudo yum install -y chromium
                ;;
            alpine)
                sudo apk add chromium
                ;;
            termux)
                echo -e "   ${WARN} Chromium is not available on Termux."
                echo "   The browser tool will be disabled, but web_fetch still works."
                ;;
            macos)
                if command -v brew &>/dev/null; then
                    brew install --cask chromium
                else
                    echo "   Install Homebrew first: https://brew.sh"
                    echo "   Then run: brew install --cask chromium"
                fi
                ;;
            *)
                echo "   Please install Chrome/Chromium manually for your system."
                ;;
        esac
    else
        echo -e "   ${WARN} Skipped. Browser tool will be disabled (web_fetch still works)."
    fi
fi
echo ""

# ---- 3. Create config if not exists ----
echo "3. Checking configuration..."
if [ -f "mclawdata/config.json" ]; then
    echo -e "   ${CHECK} config.json exists"
elif [ -f "config.example.json" ]; then
    mkdir -p mclawdata
    cp config.example.json mclawdata/config.json
    echo -e "   ${CHECK} Created mclawdata/config.json from example"
    echo -e "   ${WARN} Edit mclawdata/config.json to add your API keys and bot tokens"
else
    echo -e "   ${WARN} No config found. MClaw will create defaults on first run."
fi
echo ""

# ---- 4. Create workspace directories ----
echo "4. Setting up workspace..."
mkdir -p mclawdata/workspace/skills
mkdir -p mclawdata/sessions
mkdir -p mclawdata/memory
echo -e "   ${CHECK} Workspace directories ready"
echo ""

# ---- Summary ----
echo "============================================"
echo -e "${GREEN}🦞 Setup complete!${NC}"
echo ""
echo "Next steps:"
echo "  1. Edit mclawdata/config.json — add your API keys"
echo "  2. Run: ./mclaw start"
echo ""
echo "Minimum config needed:"
echo '  {'
echo '    "providers": { "gemini": { "api_key": "YOUR_KEY" } },'
echo '    "channels":  { "telegram": { "enabled": true, "token": "YOUR_BOT_TOKEN", "allow_from": ["YOUR_USER_ID"] } }'
echo '  }'
echo ""
