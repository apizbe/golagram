// Command wizard is examples/fsm's same /register conversation (name, then
// age, then confirm), rebuilt with gg.Wizard instead of hand-wired
// StateGroup/StateIs registrations — diff the two files to see exactly
// what the sugar removes: no manual SetState per step, no hand-wired
// /cancel route, one declarative step list. Run it with:
//
//	export BOT_TOKEN=your-token-from-botfather
//	go run ./examples/wizard
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

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("set BOT_TOKEN to a token from @BotFather")
	}

	bot, err := gg.NewTelegramBot(token)
	if err != nil {
		log.Fatal(err)
	}

	reg := gg.NewWizard("registration",
		gg.WithOnCancel(func(c *gg.Ctx) error {
			_, err := c.Answer("Cancelled.")
			return err
		}),
	)
	reg.Step(onName)
	reg.Step(onAge)
	reg.Step(onConfirm)

	r := gg.NewRouter()
	r.Include(reg.Router()) // include before the catch-all below, so wizard steps and /cancel aren't shadowed
	r.Message(gg.FilterCommand("register")).Handle(func(c *gg.Ctx) error {
		if err := reg.Enter(c); err != nil {
			return err
		}
		_, err := c.Answer("Let's get you registered! What's your name?")
		return err
	})
	r.Message().Handle(func(c *gg.Ctx) error {
		_, err := c.Answer("Send /register to get started, or /cancel to stop.")
		return err
	})
	bot.Dispatch(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("wizard bot running — press Ctrl+C to stop")
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func onName(wc *gg.WizardCtx) error {
	name := strings.TrimSpace(wc.Text())
	if name == "" {
		_, err := wc.Answer("Please send your name as text.")
		return err
	}
	if err := gg.FSMSet(wc.FSM(), "name", name); err != nil {
		return err
	}
	if _, err := wc.Answer(fmt.Sprintf("Nice to meet you, %s! How old are you?", name)); err != nil {
		return err
	}
	return wc.Next()
}

func onAge(wc *gg.WizardCtx) error {
	age, err := strconv.Atoi(strings.TrimSpace(wc.Text()))
	if err != nil || age <= 0 {
		_, err := wc.Answer("Please send your age as a number.")
		return err
	}
	if err := gg.FSMSet(wc.FSM(), "age", age); err != nil {
		return err
	}
	name, _, _ := gg.FSMGet[string](wc.FSM(), "name")
	if _, err := wc.Answer(fmt.Sprintf("Confirm: %s, age %d — reply yes or no.", name, age)); err != nil {
		return err
	}
	return wc.Next()
}

func onConfirm(wc *gg.WizardCtx) error {
	switch strings.ToLower(strings.TrimSpace(wc.Text())) {
	case "yes":
		name, _, _ := gg.FSMGet[string](wc.FSM(), "name")
		age, _, _ := gg.FSMGet[int](wc.FSM(), "age")
		if _, err := wc.Answer(fmt.Sprintf("Registered! Welcome, %s (%d).", name, age)); err != nil {
			return err
		}
		return wc.Next() // last step — Next() exits the wizard
	case "no":
		return wc.Cancel()
	default:
		_, err := wc.Answer("Please reply yes or no.")
		return err
	}
}
