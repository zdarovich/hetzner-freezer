# Hetzner Cloud Server Freezer
Tool that backups and restores Hetzner server from backup and json metadata.
Helps to save costs by freezing server and restoring it when needed.

- It serializes server metadata in json format. 
- Stortes backups of server image in Hetzner backup storage.

## Installation
```shell
git clone https://github.com/zdarovich/hetzner-freezer.git
cd hetzner-freezer
```

## Usage
```shell
go run cmd/main.go freeze --project="project-name" --token="token" --server-name="server-name"
go run cmd/main.go unfreeze --project="project-name" --token="token" --server-name="server-name"
```