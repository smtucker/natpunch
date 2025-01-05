# NAT Punchthrough Server & Client

**Notice:** This project is in a very early stage of development. Functionality
is basic and the API is subject to change.

This project aims to provide a solution for online multiplayer games to
establish connections between clients located behind Network Address Translators
(NATs). It currently includes a central server for connection orchestration, a
basic debug client, and Protobuf definitions to facilitate communication.

## Potential Goals

This project envisions becoming a comprehensive connection orchestration suite
for NAT traversal, with the following potential goals:

- **Lobby Management:** Provide a central hub for managing lobby sessions,
  allowing games to be created and joined by multiple players.
- **Enhanced NAT Traversal Techniques:** Implement support for various NAT
  traversal techniques beyond the current basic implementation, such as STUN,
  TURN, and ICE.
- **Diverse Client Support:** Facilitate integration with clients written in
  various programming languages by providing clear API documentation and
  potentially client libraries.
- **Comprehensive Connection Management:** Offer advanced features like
  connection persistence, quality-of-service management, and load balancing.
- **Advanced Security Measures:** Incorporate robust security measures,
  including encryption and authentication, to protect against unauthorized
  access and data breaches.

## Project Structure

The project is currently structured as follows:

- **Server (`/cmd/server`):** This directory contains the code for the central
  server that manages client connections and facilitates NAT punchthrough.

- **Client (`/cmd/client`):** This directory contains the code for a basic debug
  client used for testing and demonstrating the server functionality.

- **Protobuf Definitions (`/proto`):** This folder defines the Protobuf messages
  used for communication between the server, clients, and any future clients.

## Building

To build the project, simply run `make` in the root directory. This will
generate the server and client binaries in the `bin` directory.

## Usage

To run the server, execute the following command:

```bash
./bin/server -p <port>
```

Replace `<port>` with the desired port number.

To run the client, execute the following command:

```bash
./bin/client -a <server_address> -p <port>
```

Replace `<server_address>` with the IP address of the server and `<port>` with
the port number the server is listening on.
