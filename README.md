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

Client commands
- /start           — start a new round
- /bet <amount>    — place a bet for the current round
- /status          — request the server to broadcast current game state
- /help            — show available commands
- quit             — exit client

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