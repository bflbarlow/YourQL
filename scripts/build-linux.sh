#!/bin/bash
set -e

echo "=== YourQL Linux + AppImage Builder ==="
echo ""

# ---- 1. Install dependencies ----
echo "[1/5] Installing system dependencies..."
sudo apt-get update -qq

# Detect Ubuntu version to pick correct WebKit package
. /etc/os-release 2>/dev/null
if [ "${VERSION_ID:-0}" \> "24" ] 2>/dev/null || [ "${VERSION_ID:-0}" = "24.04" ]; then
    WEBKIT_PKG="libwebkit2gtk-4.1-dev"
    WEBKIT_TAG="webkit2_41"
else
    WEBKIT_PKG="libwebkit2gtk-4.0-dev"
    WEBKIT_TAG=""
fi

sudo apt-get install -y -qq build-essential libgtk-3-dev $WEBKIT_PKG imagemagick wget curl

# ---- 2. Install Go ----
if ! command -v go &>/dev/null; then
    echo "[2/5] Installing Go..."
    curl -fsSL https://go.dev/dl/go1.23.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
    echo 'export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"' >> ~/.bashrc
    export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
else
    echo "[2/5] Go already installed: $(go version)"
    export PATH="$HOME/go/bin:$PATH"
fi

# ---- 3. Install Node ----
if ! command -v node &>/dev/null; then
    echo "[3/5] Installing Node.js..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | sudo bash -
    sudo apt-get install -y -qq nodejs
else
    echo "[3/5] Node already installed: $(node --version)"
fi

# ---- 4. Build the app ----
echo "[4/5] Cloning and building YourQL..."
if [ ! -d YourQL ]; then
    git clone https://github.com/bflbarlow/YourQL.git
fi
cd YourQL
git pull

# Build the binary
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
if [ -n "$WEBKIT_TAG" ]; then
    WEBKIT_FLAGS="-tags $WEBKIT_TAG"
else
    WEBKIT_FLAGS=""
fi
wails build -platform linux/amd64 $WEBKIT_FLAGS

# ---- 5. Package as AppImage ----
echo "[5/5] Packaging AppImage..."

# Create AppDir structure
mkdir -p AppDir/usr/bin AppDir/usr/share/applications AppDir/usr/share/icons/hicolor/256x256/apps

cp build/bin/YourQL AppDir/usr/bin/yourql
chmod +x AppDir/usr/bin/yourql

cat > AppDir/usr/share/applications/yourql.desktop << 'EOF'
[Desktop Entry]
Name=YourQL
Comment=Natural language database queries
Exec=yourql
Icon=yourql
Type=Application
Categories=Development;Database;
Terminal=false
EOF
ln -sf usr/share/applications/yourql.desktop AppDir/yourql.desktop

# Generate icon
convert -size 256x256 xc:'#1a1a1a' \
    -fill '#0288d1' -draw 'roundrectangle 48,48 208,208 24,24' \
    -fill white -font DejaVu-Sans-Bold -pointsize 110 -gravity center -annotate 0 'YQ' \
    AppDir/usr/share/icons/hicolor/256x256/apps/yourql.png
ln -sf usr/share/icons/hicolor/256x256/apps/yourql.png AppDir/yourql.png

cat > AppDir/AppRun << 'EOF'
#!/bin/bash
HERE="$(dirname "$(readlink -f "$0")")"
export PATH="$HERE/usr/bin:$PATH"
export LD_LIBRARY_PATH="$HERE/usr/lib:$LD_LIBRARY_PATH"
exec "$HERE/usr/bin/yourql" "$@"
EOF
chmod +x AppDir/AppRun

# Download and run linuxdeploy
wget -q -O linuxdeploy.AppImage \
    https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage
chmod +x linuxdeploy.AppImage
wget -q https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh
chmod +x linuxdeploy-plugin-gtk.sh

./linuxdeploy.AppImage --appdir AppDir --plugin gtk --output appimage

echo ""
echo "=== Done! ==="
ls -lh YourQL-*.AppImage
echo ""
echo "Run: chmod +x YourQL-*.AppImage && ./YourQL-*.AppImage"