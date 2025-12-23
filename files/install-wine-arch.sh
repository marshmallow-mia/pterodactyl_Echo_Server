#!/bin/bash

# Arch Linux Wine Installation Script for Echo VR Server

# Enable multilib repository (should already be done in Dockerfile)
# This is needed for 32-bit wine support
if ! grep -q "^\[multilib\]" /etc/pacman.conf; then
    echo "[multilib]" >> /etc/pacman.conf
    echo "Include = /etc/pacman.d/mirrorlist" >> /etc/pacman.conf
    pacman -Syu --noconfirm
fi

# Install Wine and related packages if not already installed
pacman -S --noconfirm --needed wine wine-gecko wine-mono winetricks

# Create wine prefix and configure
export WINEARCH=win64
export WINEPREFIX=/home/container/.wine

# Initialize wine
wine wineboot --init

# Install required Windows components
winetricks -q winhttp corefonts

# Set wine to Windows 10 mode for better compatibility
winecfg -v win10

echo "Wine installation and configuration completed for Arch Linux"