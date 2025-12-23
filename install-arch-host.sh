#!/bin/bash

# Arch Linux Host Installation Script for Echo VR Server with MATE DE

echo "Starting Arch Linux installation script for Echo VR Server with MATE Desktop Environment"

# Update system
pacman -Syu --noconfirm

# Install base development tools
pacman -S --noconfirm base-devel git curl wget

# Install Docker
pacman -S --noconfirm docker docker-compose
systemctl enable docker
systemctl start docker

# Install MATE Desktop Environment
pacman -S --noconfirm mate mate-extra lightdm lightdm-gtk-greeter xorg-server xorg-xinit

# Enable display manager
systemctl enable lightdm

# Install additional tools needed for Pterodactyl
pacman -S --noconfirm nginx mariadb redis php php-fpm php-gd php-mysql php-mbstring php-bcmath php-xml php-curl php-zip composer

# Install PHP extensions
pacman -S --noconfirm php-redis

# Configure MariaDB
systemctl enable mariadb
systemctl start mariadb
mysql_secure_installation

# Configure Redis
systemctl enable redis
systemctl start redis

# Configure PHP-FPM
systemctl enable php-fpm
systemctl start php-fpm

# Configure Nginx
systemctl enable nginx

echo "Basic Arch Linux setup completed. Please continue with Pterodactyl panel installation."
echo "After completing the Pterodactyl setup, clone this repository and run the Echo VR server setup."

# Create pterodactyl user
useradd -m -s /bin/bash pterodactyl

echo "To continue:"
echo "1. Complete Pterodactyl panel installation following the official guide"
echo "2. Clone this repository: git clone https://github.com/BL00DY-C0D3/pterodactyl_Echo_Server.git"
echo "3. Build the Arch Linux Docker image: docker build -f Dockerfile.arch -t echovr-arch ."
echo "4. Follow the remaining setup instructions in the README.md"