# NAT Holepunching Server & Client

<p align="center" width="100%">
    <img src="https://github.com/user-attachments/assets/8ffd6bfe-ccdc-4dd1-9a46-25bb10d61438">
</p>

## **🚀 Blast through those pesky NATs and connect players worldwide! 🚀**

This project is a powerful tool for indie game developers who want to add seamless online multiplayer functionality to their games.  
We're building a robust NAT traversal solution that's easy to integrate and packed with features.

## **✨ MVP Ready! ✨**

The core NAT punching functionality is now implemented and ready for testing! This MVP includes:

✅ **Working Features:**
- Client registration and discovery
- NAT punching between peers
- Connection establishment and verification
- Error handling and logging
- Keep-alive mechanism
- **Lobby system for game matching**
- **Automatic peer connection when joining lobbies**
- Test scripts for validation

🧪 **Ready to Test:**
- Run `./test_mvp.sh` for basic NAT punching
- Run `./test_lobby.sh` for lobby system testing

## **💪 What We Offer**

* **Centralized Server:**  Handles all the complexities of NAT traversal for you, so you can focus on building awesome gameplay.
* **Debug Client:**  A handy tool to test connections and see the magic in action.
* **Protobuf Definitions:**  Clean and efficient communication between server and clients.

## **🔮 Future Goals**

We have big plans for this project! Here's a sneak peek at what we're cooking up:

* **Lobby Management:** Create and manage lobbies with ease, allowing players to connect and play together seamlessly.
* **Advanced NAT Traversal:**  Support for STUN, TURN, and ICE to ensure smooth connections in even the most challenging network environments.
* **Multi-Language Support:**  Integrate with clients written in various programming languages.
* **Robust Connection Management:**  Features like connection persistence, quality-of-service management, and load balancing for a top-notch multiplayer experience.
* **Ironclad Security:**  Encryption and authentication to keep your players' data safe.

## **📂 Project Structure**

* **Server (`/cmd/server`):** The heart of the operation, managing client connections and orchestrating NAT punchthrough. 
* **Client (`/cmd/client`):** A basic client for testing and demonstration.
* **Protobuf Definitions (`/proto`):**  The language of communication between server and clients.

## **🔨 Building**

It's super easy to get started! Just run `make` in the root directory to build the server and client binaries.

## **🚀 Usage**

**Fire up the server:**

```bash
./bin/server -p <port>
```

(Replace `<port>` with your desired port number.)

**Launch the client:**

```bash
./bin/client -a <server_address> -p <port>
```

(Replace `<server_address>` with the server's IP address and `<port>` with the server's port number.)

## Client Commands

The client supports the following commands:

- `list`: Lists all clients registered with the server.
- `connect <index>`: Connects to a client by its index from the `list` command.
- `ping <index>`: Pings a client by its index to verify a direct connection.
- `lobbies`: Lists all available lobbies.
- `create <name> <max_players>`: Creates a new lobby.
- `join <index>`: Joins a lobby by its index from the `lobbies` command.
- `help`: Displays a list of available commands.
- `exit`: Exits the client.

## Testing

The `test_mvp.sh` and `test_lobby.sh` scripts can be used to test the NAT punching and lobby functionality.

Once connected, you can use the `ping` command to verify that a direct peer-to-peer connection has been established.

**Example:**

1.  Run `./test_mvp.sh` to start the server and two clients.
2.  In one client, type `list` to see the other client's index.
3.  Type `connect <index>` to initiate the NAT punch-through.
4.  Type `ping <index>` to send a direct ping to the other client.

A successful ping will be logged in both clients, confirming a direct connection.

## **🙌 Join the Community**

We'd love to hear from you!  Contribute to the project, report issues, or share your ideas. Let's build amazing multiplayer games together! 🧑‍🤝‍🧑
