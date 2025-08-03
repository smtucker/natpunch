#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -e
set -x

# Build server and client
make all

# Start server in the background
./bin/server -a 127.0.0.1 -p 8081 &
SERVER_PID=$!
sleep 1

# Client 1 creates a lobby
CLIENT1_OUTPUT=$(./bin/client -a 127.0.0.1 -p 8081 <<EOF
create test_lobby 2
help
exit
EOF
)
echo "$CLIENT1_OUTPUT"
LOBBY_ID=$(echo "$CLIENT1_OUTPUT" | grep "Lobby ID" | awk '{print $5}')

if [ -z "$LOBBY_ID" ]; then
    echo "Failed to create lobby"
    kill $SERVER_PID
    exit 1
fi

# Client 2 joins the lobby
./bin/client -a 127.0.0.1 -p 8081 <<EOF | tee /tmp/client2_output
join 0
members
exit
EOF

# Check if client 2 sees both members
if ! grep -q "2" /tmp/client2_output; then
    echo "Client 2 failed to see both members in the lobby"
    kill $SERVER_PID
    exit 1
fi

# Client 1 leaves the lobby
./bin/client -a 127.0.0.1 -p 8081 <<EOF
leave
exit
EOF

# Client 2 checks members again
./bin/client -a 127.0.0.1 -p 8081 <<EOF | tee /tmp/client2_output_after_leave
members
exit
EOF

# Check if client 2 sees only one member
if ! grep -q "1" /tmp/client2_output_after_leave; then
    echo "Client 2 failed to see only one member after client 1 left"
    kill $SERVER_PID
    exit 1
fi

echo "Lobby test passed!"

# Cleanup
kill $SERVER_PID
rm /tmp/client2_output /tmp/client2_output_after_leave
