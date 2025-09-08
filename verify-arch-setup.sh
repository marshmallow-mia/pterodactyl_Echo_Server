#!/bin/bash

# Verification script for Arch Linux Echo VR setup

echo "🔍 Verifying Arch Linux Echo VR setup..."
echo "========================================="

# Check if we're on Arch Linux
if command -v pacman &> /dev/null; then
    echo "✅ Arch Linux detected"
else
    echo "⚠️  Not running on Arch Linux - this script is designed for Arch"
fi

# Check Docker installation
if command -v docker &> /dev/null; then
    echo "✅ Docker is installed"
    docker --version
else
    echo "❌ Docker is not installed"
fi

# Check if MATE is installed
if pacman -Qs mate &> /dev/null; then
    echo "✅ MATE Desktop Environment is installed"
else
    echo "❌ MATE Desktop Environment is not installed"
fi

# Check if LightDM is installed and enabled
if systemctl is-enabled lightdm &> /dev/null; then
    echo "✅ LightDM display manager is enabled"
else
    echo "⚠️  LightDM display manager is not enabled"
fi

# Check for wine installation
if command -v wine &> /dev/null; then
    echo "✅ Wine is installed"
    wine --version
else
    echo "❌ Wine is not installed"
fi

# Check for required files
echo ""
echo "🔍 Checking required files..."

files=(
    "Dockerfile.arch"
    "Dockerfile.arch.alt"
    "files/install-wine-arch.sh"
    "scripts/entrypoint-arch.sh"
    "egg-echo-arch.json"
    "README-ARCH.md"
    "VNC-SETUP.md"
    "build-arch.sh"
    "install-arch-host.sh"
)

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo "✅ $file exists"
    else
        echo "❌ $file is missing"
    fi
done

# Check if echovr-arch Docker image exists
echo ""
echo "🔍 Checking Docker images..."
if docker images | grep -q "echovr-arch"; then
    echo "✅ echovr-arch Docker image found"
else
    echo "⚠️  echovr-arch Docker image not found - run ./build-arch.sh to build it"
fi

echo ""
echo "🔍 Verification complete!"
echo ""
echo "Next steps:"
echo "1. If any components are missing, install them using install-arch-host.sh"
echo "2. Build the Docker image using ./build-arch.sh"
echo "3. Follow README-ARCH.md for complete setup instructions"