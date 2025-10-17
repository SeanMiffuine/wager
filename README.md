# My Go Project

## Overview
This project is a Go application that provides a set of functionalities through a well-structured architecture. It includes various components such as handlers, models, services, and utility functions, organized into appropriate directories.

Wager — terminal multiplayer bribing game

Overview

This repository contains a simple terminal-based multiplayer game (over WebSockets).
Players chat and place bets each round. The highest bet wins a "bribe" for that round.
The player with most bribes at game end is the winner (REAL DICTATOR OF AMERICA).

Rules (summary)
- 2 - 32 players
- All players start with an initial USD (default $1000)
- Each round players can wager 0 up to their USD
- Highest wager(s) win the bribe (ties allowed)
- Everyone pays their wager regardless of win/loss
- When only one player has money left, they receive 1 automatic bribe
- Game ends when players run out of money or last player condition triggers

How to run (local)

Start server (local):

```bash
cd /path/to/wager
go run ./cmd/app -s -l
```

Flags for server:
- -round <seconds> : duration of each betting round (default 20)
- -initial <USD>   : initial USD for each player (default 1000)

Start a client (in separate terminal):

```bash
cd /path/to/wager
go run ./cmd/app
# enter username when prompted
```

Client commands (room workflow)

Workflow notes:
- Players can create or join a room using a short human-friendly code.
- The player who creates a room becomes the host and is automatically joined to that room.

Commands:
- /create [CODE]   — create a new room. Optionally provide a short code (e.g. /create ABC1). If no code is provided, the server generates a friendly code (e.g. K7QP) and replies with `room_created`.
- /join CODE       — join an existing room with the given code (e.g. /join K7QP). Server replies with `room_joined` or `error` if not found.
- /start           — start the negotiation + betting cycle for your current room (host or any player depending on room rules). The room will broadcast `round_start` and start the negotiation timer.
- /bet <amount>    — submit a wager for the current room's betting phase (must be <= your USD). Server validates and replies with `bet_confirm` or `error`.
- /status          — request the server to broadcast the current game state for your room.
- /help            — show available commands
- quit             — exit client

Examples:
- Host creates a room and gets the code back:
	- Client types: `/create` -> server replies `room_created K7QP`
	- Other players: `/join K7QP` to join that room.
- Typical round flow in a room:
	- Host or any player issues `/start`
	- Negotiation timer runs (chat allowed)
	- After timer, players place `/bet 200` (or 0)
	- When resolved, server broadcasts `round_result` and the next negotiation begins


Testing

Run unit tests for the game logic:

```bash
cd /path/to/wager
go test ./cmd/app
```

Notes & next steps
- Usernames are made unique on the server if collisions occur (a suffix is appended).
- Server broadcasts join/leave notifications and maintains player online state and join order.
- Bet payloads are validated on the server; malformed bets receive an error message.

Contributions welcome — open an issue or PR with improvements (UI, persistent leaderboard, better CLI).