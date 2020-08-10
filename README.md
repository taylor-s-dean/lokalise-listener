# lokalise-listener
This is a web server that listens to incoming webhooks from Lokalise and creates pull requests.

## Status
![Go](https://github.com/limitz404/lokalise-listener/workflows/Go/badge.svg)

## Installation Instructions
Clone git repository:
```sh
git clone https://github.com/limitz404/lokalise-listener.git
```

Build the executable:
```sh
go get && go build
```

Export environment variables:
```sh
export TLS_CERTIFICATE_PATH='<path/to/fullchain.pem>'
export TLS_PRIVATE_KEY_PATH='<path/to/privkey.pem>'
export LOKALISE_WEBHOOK_SECRET='<redacted>'
export LOKALISE_READ_ONLY_API_TOKEN='<redacted>'
```

Run executable:
(without sudo)
```sh
./lokalise-listener
```
(with sudo):
```sh
sudo -E ./lokalise-listener
```

## Creating TLS certificates
Install `certbot`
```sh
sudo apt install certbot
```

Generate certificates:
```sh
sudo certbot certonly --standalone -d <list of domains> --agree-tos --non-interactive -m <email> --rsa-key-size 4096
```
