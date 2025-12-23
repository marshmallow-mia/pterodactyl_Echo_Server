#!/bin/bash

# Build script for Arch Linux Echo VR Docker image

echo "Building Echo VR Server Docker image for Arch Linux with MATE DE..."

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed. Please install Docker first."
    exit 1
fi

# Function to try building with different Dockerfiles
build_image() {
    local dockerfile=$1
    local attempt=$2
    
    echo "Attempt $attempt: Building with $dockerfile"
    docker build -f $dockerfile -t echovr-arch .
    return $?
}

# Try standard Dockerfile first
if build_image "Dockerfile.arch" "1"; then
    echo "✅ Docker image built successfully with standard Dockerfile!"
elif build_image "Dockerfile.arch.alt" "2"; then
    echo "✅ Docker image built successfully with alternative Dockerfile!"
else
    echo "❌ All Docker build attempts failed!"
    echo ""
    echo "Common issues and solutions:"
    echo "1. Network connectivity issues - ensure internet access for package downloads"
    echo "2. Insufficient disk space - Docker build requires several GB of space"
    echo "3. Missing dependencies - ensure files/install-wine-arch.sh exists"
    echo ""
    echo "If network issues persist, you may need to build on a system with internet access"
    echo "or use a different base image with pre-installed packages."
    exit 1
fi

echo ""
echo "Next steps:"
echo "1. Use 'echovr-arch' as the Docker image in your Pterodactyl egg configuration"
echo "2. Import the egg-echo-arch.json file in your Pterodactyl panel"
echo "3. Follow the README-ARCH.md guide for complete setup"
echo ""
echo "To test the image:"
echo "docker run --rm echovr-arch echo 'Arch Linux Echo VR container is working!'"