FROM debian:bullseye-slim as build

ENV DEBIAN_FRONTEND="noninteractive"

#Install some needed packages
RUN apt-get update \
 && apt-get install -y wget software-properties-common gnupg2 cabextract procps bc htop nano curl adduser

RUN apt purge -y sudo


RUN addgroup --gid 994 container && \
    adduser --uid 999 --gid 994 --home /home/container --disabled-password --gecos "" container && \
    mkdir -p /home/container && \
    chown -R container:container /home/container




RUN echo "container ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers




USER root

WORKDIR /home/container


# Clean up APT-Caches
RUN apt-get -y autoremove \
 && apt-get clean autoclean \
 && rm -rf /tmp/* /var/tmp/* /var/lib/apt/lists

# Install wine and winetricks
COPY ./files/install-wine.sh /
RUN bash /install-wine.sh \
 && rm /install-wine.sh



# SET the Echo Folder
RUN wine wineboot

ARG src="./files/demoprofile.json"
ARG target="/root/.wine/drive_c/users/root/Local Settings/Application Data/rad/echovr/users/dmo/demoprofile.json"
ARG target2="/home/container/.wine/drive_c/users/container/Local Settings/Application Data/rad/echovr/users/dmo/demoprofile.json"
COPY ${src} ${target}
COPY ${src} ${target2}

RUN chown -R container:container /home/container

ENV WINEDEBUG=-all
ENV TERM=xterm
USER container
ENV  USER=container HOME=/home/container
ENTRYPOINT ["bash", "/scripts/entrypoint.sh"]
