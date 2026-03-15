#!/bin/bash
set -e

# 1. Create directory for SSL certificates
mkdir -p /app/certs
cd /app/certs

echo "[INFO] Generating self-signed certificates..."
# 2. Generate Private Key and Self-Signed Certificate
# -nodes: Don't encrypt the key (no password required on startup)
# -subj: Pre-fills the certificate info to avoid interactive prompts
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -sha256 -days 365 -nodes \
  -subj "/C=US/ST=State/L=City/O=Organization/OU=Unit/CN=firecracker.local"

echo "[INFO] Certificates generated in /app/certs"

# 3. Create the Node.js HTTPS server
cat > /app/https-server.js <<'EOF'
const https = require('https');
const fs = require('fs');
const path = require('path');

const options = {
  key: fs.readFileSync(path.join(__dirname, 'certs/key.pem')),
  cert: fs.readFileSync(path.join(__dirname, 'certs/cert.pem'))
};

const server = https.createServer(options, (req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/plain' });
  res.end(`Hello from Secure Firecracker!\nProtocol: ${req.protocol || 'https'}\nNode: ${process.version}\n`);
});

const PORT = 443;
server.listen(PORT, '0.0.0.0', () => {
  console.log(`HTTPS Server running at https://0.0.0.0:${PORT}/`);
});
EOF

echo "[INFO] Starting HTTPS server (requires sudo/root for port 443)..."
# 4. Start the server
# Note: Since your init script runs as root, this is fine.
node /app/https-server.js
