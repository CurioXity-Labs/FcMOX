#!/bin/bash
(
echo '{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}}'
sleep 1
echo '{"jsonrpc": "2.0", "id": 2, "method": "notifications/initialized"}'
sleep 1
echo '{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "sandbox_list_vms", "arguments": {}}}'
sleep 1
) | sudo /home/n/firecracker/mcp-server/mcp-sandbox --firecracker=/home/n/firecracker/firecracker --kernel=/home/n/firecracker/lk-images/vmlinux-6.12-ebpf --rootfs=/home/n/firecracker/lk-rootfs/debian.ext4 --rootfs-dir=/home/n/firecracker --ssh-user=root --ssh-password=root > /tmp/mcp-test-stdout.log 2> /tmp/mcp-test-stderr.log
