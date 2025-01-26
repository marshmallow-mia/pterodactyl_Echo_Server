# Echo Server in Pterodactyl

This Guide will help you set up Echo Servers on Linux with Pterodactyl.

Please use the same paths as I do in this guide. If you dont, stuff might now work as expected.
You also need to have a Domain for this Guide to work.
You might be able to do it without, but you have to figure out how by yourself.


I use Debian 12 x86. Other systems should work, but in this Guide I will only show how to get it to run on Debian 12.

To install the basic Pterodactyl instance, please follow the official Guide.
It doesnt make sense for me to just repeat the guide here.
I will provide some additional imformations for some of the steps, if needed.

Before you start you should update your system
```
apt update && apt upgrade -y
```
On the Dependency Install, you have to install PHP a different way, as the official Guide uses Ubuntu

I will provide the way I installed the dependencies, but **you have to** check the official Guide for the correct versions. Especially for php
```
apt install -y curl gnupg lsb-release apt-transport-https ca-certificates
wget -qO - https://packages.sury.org/php/apt.gpg | sudo gpg --dearmor -o /usr/share/keyrings/sury-php.gpg
echo "deb [signed-by=/usr/share/keyrings/sury-php.gpg] https://packages.sury.org/php bookworm main" | sudo tee /etc/apt/sources.list.d/sury-php.list
apt update
curl -LsS https://r.mariadb.com/downloads/mariadb_repo_setup | sudo bash
apt update
#Check for the right php version on the official Guide!
sudo apt install -y php8.3 php8.3-{common,cli,gd,mysql,mbstring,bcmath,xml,fpm,curl,zip} mariadb-server nginx tar unzip git redis-server
curl -sS https://getcomposer.org/installer | sudo php -- --install-dir=/usr/local/bin --filename=composer
```
Now continue on the "Download Files" Section of the official Guide:
```
https://pterodactyl.io/panel/1.0/getting_started.html
```
