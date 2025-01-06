# NAT Punchthrough Server & Client 🎮

**🚀 Blast through those pesky NATs and connect players worldwide! 🚀**

This project is a powerful tool for indie game developers who want to add seamless online multiplayer functionality to their games.  
We're building a robust NAT traversal solution that's easy to integrate and packed with features.

**✨ Currently in Early Development ✨**

This project is just getting started, but check back often! We're actively working on this project, adding exciting new features and improving existing ones.
While it's still in its very early stages, the core functionality is in place and ready for you to experiment with!

**💪 What We Offer**

* **Centralized Server:**  Handles all the complexities of NAT traversal for you, so you can focus on building awesome gameplay.
* **Debug Client:**  A handy tool to test connections and see the magic in action.
* **Protobuf Definitions:**  Clean and efficient communication between server and clients.

**🔮 Future Goals**

We have big plans for this project! Here's a sneak peek at what we're cooking up:

* **Lobby Management:** Create and manage lobbies with ease, allowing players to connect and play together seamlessly.
* **Advanced NAT Traversal:**  Support for STUN, TURN, and ICE to ensure smooth connections in even the most challenging network environments.
* **Multi-Language Support:**  Integrate with clients written in various programming languages.
* **Robust Connection Management:**  Features like connection persistence, quality-of-service management, and load balancing for a top-notch multiplayer experience.
* **Ironclad Security:**  Encryption and authentication to keep your players' data safe.

**📂 Project Structure**

* **Server (`/cmd/server`):** The heart of the operation, managing client connections and orchestrating NAT punchthrough. 
* **Client (`/cmd/client`):** A basic client for testing and demonstration.
* **Protobuf Definitions (`/proto`):**  The language of communication between server and clients.

**🔨 Building**

It's super easy to get started! Just run `make` in the root directory to build the server and client binaries.

**🚀 Usage**

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

**🙌 Join the Community**

We'd love to hear from you!  Contribute to the project, report issues, or share your ideas. Let's build amazing multiplayer games together! 🧑‍🤝‍🧑
