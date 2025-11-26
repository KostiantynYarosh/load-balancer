# Go Load Balancer

HTTPS load balancer with least-connections algorithm and health checking.

## Features

- **Least-connections balancing** — routes to server with lowest load relative to its limit
- **Health checks** — polls `/health` endpoint every 2 minutes
- **TLS termination** — HTTPS on load balancer, HTTP to backends

## Configuration

`servers.json`:
```json
{
  "Servers": [
    {"Id": 1, "MaximumActiveConnections": 5, "Status": true, "URL": "http://localhost:9001"},
    {"Id": 2, "MaximumActiveConnections": 3, "Status": true, "URL": "http://localhost:9002"}
  ]
}
```

## Usage
```bash
# Start test backends (stubs for simulation)
go run testserver/main.go -port 9001 -time 2
go run testserver/main.go -port 9002 -time 1

# Start load balancer
go run main.go -servers servers.json
```

Load balancer listens on `https://localhost:8443`
![Screenshot](https://media.discordapp.net/attachments/874216555670556675/1443136196741169212/dds1.png?ex=6927f8d1&is=6926a751&hm=311edaede1f338f3b8f261726666f3f6cbf5ca47a1f683f4997681c732e04c1a&=&format=webp&quality=lossless)