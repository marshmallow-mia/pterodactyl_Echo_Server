# Echo Server in Pterodactyl - Arch Linux with MATE Desktop Environment

This is an Arch Linux adaptation of the Echo VR server setup for Pterodactyl with MATE Desktop Environment support.

![image](https://github.com/user-attachments/assets/983bed6e-7714-44a2-96be-1517073eeabe)

## Arch Linux Setup

This guide provides setup instructions for Arch Linux with MATE Desktop Environment.

You should have a domain for the Pterodactyl Dashboard to be able to use SSL/https. You might be able to do it without, but this guide will work with a domain.

### Prerequisites

- Fresh Arch Linux installation
- Root or sudo access
- Domain name (recommended)

### System Preparation

First, update your Arch Linux system:
```bash
pacman -Syu
```

### Automated Installation

Run the automated installation script:
```bash
curl -o install-arch-host.sh https://raw.githubusercontent.com/BL00DY-C0D3/pterodactyl_Echo_Server/main/install-arch-host.sh
chmod +x install-arch-host.sh
sudo ./install-arch-host.sh
```

This script will install:
- MATE Desktop Environment
- Docker and Docker Compose
- Web server components (Nginx, PHP, MariaDB, Redis)
- Development tools

### MATE Desktop Environment

The MATE Desktop Environment will be installed and configured with LightDM as the display manager. After installation:

1. Reboot your system
2. You'll be greeted with the LightDM login screen
3. Log in to access the MATE desktop

To start the desktop environment manually:
```bash
systemctl start lightdm
```

### Docker Configuration

The Arch Linux Docker image is available in two versions:

#### Option 1: Standard Dockerfile (Dockerfile.arch)
```bash
cd /opt/pterodactyl_Echo_Server
docker build -f Dockerfile.arch -t echovr-arch .
```

#### Option 2: Alternative Dockerfile (Dockerfile.arch.alt)
If the standard build fails due to network issues:
```bash
cd /opt/pterodactyl_Echo_Server
docker build -f Dockerfile.arch.alt -t echovr-arch .
```

#### Option 3: Automated Build Script
```bash
cd /opt/pterodactyl_Echo_Server
./build-arch.sh
```

**Note**: Docker builds may fail in environments with limited network connectivity. If you encounter package download errors, try building on a system with full internet access or use the alternative Dockerfile.

### Pterodactyl Panel Installation

Follow the official Pterodactyl installation guide, but use these Arch-specific commands:

#### Dependencies Installation
```bash
# Install required packages
pacman -S --noconfirm curl wget tar unzip git redis mariadb nginx
pacman -S --noconfirm php php-fpm php-gd php-mysql php-mbstring php-bcmath php-xml php-curl php-zip
pacman -S --noconfirm composer

# Enable and start services
systemctl enable --now mariadb redis php-fpm nginx

# Secure MariaDB installation
mysql_secure_installation
```

#### Database Setup
```bash
# Create database
mysql -u root -p
CREATE DATABASE pterodactyl;
CREATE USER 'pterodactyl'@'localhost' IDENTIFIED BY 'strong_password';
GRANT ALL PRIVILEGES ON pterodactyl.* TO 'pterodactyl'@'localhost';
FLUSH PRIVILEGES;
EXIT;
```

#### Download Pterodactyl Panel
```bash
cd /var/www
git clone https://github.com/pterodactyl/panel.git pterodactyl
cd pterodactyl
composer install --no-dev --optimize-autoloader
```

Continue with the official Pterodactyl guide for environment setup and configuration.

### Echo VR Server Configuration

After completing the Pterodactyl setup:

1. Clone this repository:
```bash
cd /opt
git clone https://github.com/BL00DY-C0D3/pterodactyl_Echo_Server.git
cd pterodactyl_Echo_Server
```

2. Build the Arch Linux Docker image:
```bash
docker build -f Dockerfile.arch -t echovr-arch .
```

3. Run the binary download script:
```bash
bash getBinaries.sh
```

4. Configure the Echo settings as described in the main README.md

### Creating Pterodactyl Egg for Arch Linux

When creating the egg in Pterodactyl:
- Use the Docker image: `echovr-arch` (or your custom image name)
- Use the same configuration as described in the main README.md

### Desktop Environment Features

With MATE Desktop Environment installed, you can:
- Use remote desktop connections (VNC, RDP)
- Run GUI applications for server management
- Access graphical tools for monitoring and configuration
- Use desktop applications alongside the Echo VR server

To enable remote desktop access, see the detailed [VNC Setup Guide](VNC-SETUP.md).

Quick VNC setup:
```bash
# Install VNC server
pacman -S --noconfirm tigervnc

# Set VNC password
vncpasswd

# Start VNC server
vncserver :1 -geometry 1920x1080 -depth 24
```

### Troubleshooting

#### Wine Issues
If wine installation fails, manually install:
```bash
pacman -S --noconfirm wine wine-gecko wine-mono winetricks
```

#### Desktop Environment Issues
If MATE doesn't start:
```bash
systemctl status lightdm
systemctl restart lightdm
```

#### Docker Permission Issues
Add your user to the docker group:
```bash
usermod -aG docker $USER
```

### Notes

- This setup provides both server functionality and desktop environment
- MATE DE is lightweight and suitable for server environments
- Remote desktop capability allows for easy management
- All original Echo VR server functionality is preserved

For the complete setup process, refer to both this guide and the main README.md file.