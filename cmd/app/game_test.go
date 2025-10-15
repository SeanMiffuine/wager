package main

import (
	"testing"
)

func TestResolveRound_SimpleWinner(t *testing.T) {
	h := newHub()
	h.game = &Game{}
	h.game.Players = map[string]*PlayerState{
		"alice": {Username: "alice", USD: 1000, Bribes: 0, Online: true},
		"bob":   {Username: "bob", USD: 1000, Bribes: 0, Online: true},
	}
	h.game.Bets = map[string]int{"alice": 200, "bob": 100}
	h.game.Active = true
	h.game.Round = 1

	h.resolveRound()

	if h.game.Players["alice"].Bribes != 1 {
		t.Fatalf("expected alice to have 1 bribe, got %d", h.game.Players["alice"].Bribes)
	}
	if h.game.Players["alice"].USD != 800 {
		t.Fatalf("expected alice USD 800, got %d", h.game.Players["alice"].USD)
	}
	if h.game.Players["bob"].USD != 900 {
		t.Fatalf("expected bob USD 900, got %d", h.game.Players["bob"].USD)
	}
}

func TestResolveRound_TieWinners(t *testing.T) {
	h := newHub()
	h.game = &Game{}
	h.game.Players = map[string]*PlayerState{
		"alice": {Username: "alice", USD: 500, Bribes: 0, Online: true},
		"bob":   {Username: "bob", USD: 500, Bribes: 0, Online: true},
		"carol": {Username: "carol", USD: 500, Bribes: 0, Online: true},
	}
	h.game.Bets = map[string]int{"alice": 200, "bob": 200, "carol": 100}
	h.game.Active = true
	h.game.Round = 2

	h.resolveRound()

	if h.game.Players["alice"].Bribes != 1 || h.game.Players["bob"].Bribes != 1 {
		t.Fatalf("expected alice and bob to each have 1 bribe")
	}
	if h.game.Players["carol"].Bribes != 0 {
		t.Fatalf("expected carol to have 0 bribes")
	}
}

func TestResolveRound_LastPlayerAutoBribe(t *testing.T) {
	h := newHub()
	h.game = &Game{}
	h.game.Players = map[string]*PlayerState{
		"solo": {Username: "solo", USD: 10, Bribes: 0, Online: true},
	}
	h.game.Bets = map[string]int{}
	h.game.Active = true
	h.game.Round = 3

	h.resolveRound()

	if h.game.Players["solo"].Bribes != 1 {
		t.Fatalf("expected solo to have 1 bribe when last player, got %d", h.game.Players["solo"].Bribes)
	}
}
