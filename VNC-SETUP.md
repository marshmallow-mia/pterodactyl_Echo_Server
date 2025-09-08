# VNC Remote Desktop Setup for Arch Linux Echo VR Server

This guide explains how to set up VNC remote desktop access for managing your Echo VR server running on Arch Linux with MATE Desktop Environment.

## Installation

### On Host System (Arch Linux)

Install TigerVNC server:
```bash
pacman -S --noconfirm tigervnc
```

### VNC Server Configuration

1. **Set VNC password:**
```bash
vncpasswd
```

2. **Create VNC configuration:**
```bash
mkdir -p ~/.vnc
echo "mate-session" > ~/.vnc/xstartup
chmod +x ~/.vnc/xstartup
```

3. **Start VNC server:**
```bash
vncserver :1 -geometry 1920x1080 -depth 24
```

4. **Auto-start VNC on boot (optional):**
```bash
# Create systemd service
sudo tee /etc/systemd/system/vncserver@.service << EOF
[Unit]
Description=Remote desktop service (VNC)
After=syslog.target network.target

[Service]
Type=forking
User=your_username
ExecStartPre=/bin/sh -c '/usr/bin/vncserver -kill %i > /dev/null 2>&1 || :'
ExecStart=/usr/bin/vncserver %i -geometry 1920x1080 -depth 24
ExecStop=/usr/bin/vncserver -kill %i
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start the service
sudo systemctl enable vncserver@:1.service
sudo systemctl start vncserver@:1.service
```

## Docker Container VNC Setup

The Docker containers are configured to support VNC when the `VNC_ENABLED` environment variable is set to `true`.

### Pterodactyl Configuration

In your Pterodactyl server settings, add these environment variables:

```
VNC_ENABLED=true
DISPLAY=:1
```

### Port Forwarding

Add VNC port (5901) to your server allocation in Pterodactyl:
- Go to Servers → Your Server → Network
- Add allocation for port 5901

## Connecting to VNC

### Windows Clients
- **TightVNC Viewer**: Free and reliable
- **RealVNC Viewer**: Feature-rich option
- **UltraVNC**: Open source alternative

### Linux Clients
```bash
# Using Remmina
pacman -S remmina
remmina

# Using TigerVNC viewer
pacman -S tigervnc
vncviewer your-server-ip:5901
```

### macOS Clients
- **Screen Sharing**: Built into macOS (Connect to vnc://your-server-ip:5901)
- **RealVNC Viewer**: Cross-platform option

## Connection Details

- **Server Address**: `your-server-ip:5901`
- **Port**: `5901` (for display :1)
- **Password**: The password you set with `vncpasswd`

## Troubleshooting

### VNC Server Won't Start
```bash
# Check if display is already in use
ps aux | grep vnc

# Kill existing VNC sessions
vncserver -kill :1

# Check VNC log
cat ~/.vnc/*.log
```

### Connection Refused
```bash
# Check if VNC server is listening
netstat -tlnp | grep :5901

# Check firewall settings
sudo ufw allow 5901/tcp
```

### Black Screen in VNC
```bash
# Check xstartup file
cat ~/.vnc/xstartup

# Restart VNC with correct session
echo "mate-session" > ~/.vnc/xstartup
chmod +x ~/.vnc/xstartup
vncserver -kill :1
vncserver :1 -geometry 1920x1080 -depth 24
```

### Performance Issues
- Use 16-bit color depth: `-depth 16`
- Lower resolution: `-geometry 1366x768`
- Enable compression in VNC client settings

## Security Considerations

1. **Change default VNC port:**
```bash
vncserver :2 -geometry 1920x1080 -depth 24  # Uses port 5902
```

2. **Use SSH tunnel:**
```bash
ssh -L 5901:localhost:5901 user@your-server-ip
# Then connect VNC client to localhost:5901
```

3. **Limit VNC access:**
```bash
# Only allow VNC connections from localhost
vncserver :1 -localhost -geometry 1920x1080 -depth 24
```

4. **Configure firewall:**
```bash
# Allow VNC only from specific IP
sudo ufw allow from 192.168.1.0/24 to any port 5901
```

## Integration with Echo VR Management

With VNC access, you can:
- Monitor Echo VR server logs in real-time using graphical log viewers
- Use GUI tools for server configuration
- Access web-based Pterodactyl panel through desktop browsers
- Run diagnostic tools with graphical interfaces
- Manage files using graphical file managers

This provides a complete desktop experience for Echo VR server administration while maintaining all the benefits of the containerized setup.