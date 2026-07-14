// Command fsm shows golagram's conversation model: a tiny /register wizard
// (name, then age, then confirm) built from a StateGroup and FSMGet/FSMSet.
// /cancel bails out of any conversation in progress. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/fsm
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	gg "github.com/apizbe/golagram"
)

var Reg = gg.StateGroup("registration")

var (
	RegName    = Reg.New("name")
	RegAge     = Reg.New("age")
	RegConfirm = Reg.New("confirm")
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("set BOT_TOKEN to a token from @BotFather")
	}

	bot, err := gg.NewTelegramBot(token)
	if err != nil {
		log.Fatal(err)
	}

	r := gg.NewRouter()
	r.Message(gg.FilterCommand("cancel")).Handle(func(c *gg.Ctx) error {
		if err := c.FSM().Clear(); err != nil {
			return err
		}
		_, err := c.Answer("Cancelled.")
		return err
	})
	r.Message(gg.FilterCommand("register")).Handle(func(c *gg.Ctx) error {
		if err := c.FSM().SetState(RegName); err != nil {
			return err
		}
		_, err := c.Answer("Let's get you registered! What's your name?")
		return err
	})
	r.Message(gg.StateIs(RegName)).Handle(onName)
	r.Message(gg.StateIs(RegAge)).Handle(onAge)
	r.Message(gg.StateIs(RegConfirm)).Handle(onConfirm)
	r.Message().Handle(func(c *gg.Ctx) error {
		_, err := c.Answer("Send /register to get started, or /cancel to stop.")
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("fsm bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func onName(c *gg.Ctx) error {
	name := strings.TrimSpace(c.Text())
	if name == "" {
		_, err := c.Answer("Please send your name as text.")
		return err
	}
	if err := gg.FSMSet(c.FSM(), "name", name); err != nil {
		return err
	}
	if err := c.FSM().SetState(RegAge); err != nil {
		return err
	}
	_, err := c.Answer(fmt.Sprintf("Nice to meet you, %s! How old are you?", name))
	return err
}

func onAge(c *gg.Ctx) error {
	age, err := strconv.Atoi(strings.TrimSpace(c.Text()))
	if err != nil || age <= 0 {
		_, err := c.Answer("Please send your age as a number.")
		return err
	}
	if err := gg.FSMSet(c.FSM(), "age", age); err != nil {
		return err
	}
	if err := c.FSM().SetState(RegConfirm); err != nil {
		return err
	}
	name, _, _ := gg.FSMGet[string](c.FSM(), "name")
	_, sendErr := c.Answer(fmt.Sprintf("Confirm: %s, age %d — reply yes or no.", name, age))
	return sendErr
}

func onConfirm(c *gg.Ctx) error {
	switch strings.ToLower(strings.TrimSpace(c.Text())) {
	case "yes":
		name, _, _ := gg.FSMGet[string](c.FSM(), "name")
		age, _, _ := gg.FSMGet[int](c.FSM(), "age")
		if err := c.FSM().Clear(); err != nil {
			return err
		}
		_, err := c.Answer(fmt.Sprintf("Registered! Welcome, %s (%d).", name, age))
		return err
	case "no":
		if err := c.FSM().Clear(); err != nil {
			return err
		}
		_, err := c.Answer("Cancelled. /register to try again.")
		return err
	default:
		_, err := c.Answer("Please reply yes or no.")
		return err
	}
}
