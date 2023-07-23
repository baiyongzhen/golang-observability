#!/bin/bash

set -e

# Install Docker
curl -fsSL get.docker.com -o get-docker.sh
sh get-docker.sh
sudo usermod -aG docker vagrant

# Install Docker Compose
sudo  curl -SL https://github.com/docker/compose/releases/download/v2.14.2/docker-compose-linux-x86_64 -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose


# Install OpenJDK
sudo apt-get -y install openjdk-11-jdk

# Install pip
sudo apt-get -y install python-pip
sudo apt-get -y install python3-pip

# Disable firewall
sudo ufw disable


# Install Golang
# golang version number
GO_VERSION=1.20
sudo apt-get install -y curl
sudo curl -fsSL "https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz" | sudo tar Cxz /usr/local

cat >> /home/vagrant/.profile <<EOF
GOPATH=\\$HOME/go
PATH=/usr/local/go/bin:\\$PATH
export GOPATH PATH
EOF

source /home/vagrant/.profile


# Install Docker Plugin
docker plugin install grafana/loki-docker-driver:latest --alias loki --grant-all-permissions
sudo apt install siege -y