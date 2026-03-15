#!/bin/bash
(
echo '{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}}'
sleep 1
echo '{"jsonrpc": "2.0", "id": 2, "method": "notifications/initialized"}'
sleep 1
echo '{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "sandbox_list_vms", "arguments": {}}}'
sleep 1
) | sudo /home/satyam/personal_workspace/AI-Sandboxing-with-microvm/latest-firecracker-ebpf/mcp-server/mcp-sandbox --firecracker=/home/satyam/personal_workspace/AI-Sandboxing-with-microvm/latest-firecracker-ebpf/firecracker --kernel=/home/satyam/personal_workspace/AI-Sandboxing-with-microvm/latest-firecracker-ebpf/lk-images/vmlinux-6.12-ebpf --rootfs=/home/satyam/personal_workspace/AI-Sandboxing-with-microvm/latest-firecracker-ebpf/lk-rootfs/debian.ext4 --rootfs-dir=/home/satyam/personal_workspace/AI-Sandboxing-with-microvm/latest-firecracker-ebpf --ssh-user=root --ssh-password=root > /tmp/mcp-test-stdout.log 2> /tmp/mcp-test-stderr.log
