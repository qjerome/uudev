package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	reVar = regexp.MustCompile(`^\w+=.*`)

	uudevRun = fmt.Sprintf("/var/run/user/%d/uudev", os.Getuid())
	uudevPid = filepath.Join(uudevRun, "uudev.pid")

	defaultDelay = 3 * time.Second
)

type Rule struct {
	Name      string            `yaml:"name"`
	Env       map[string]string `yaml:"env"`
	Run       string            `yaml:"run"`
	Delay     string            `yaml:"delay"`
	NoTimeout bool              `yaml:"no-timeout"`
}

func (r *Rule) Compile() (c *cRule, err error) {
	c = &cRule{
		Env:   make(map[string]*regexp.Regexp),
		Delay: defaultDelay,
	}
	c.Name = r.Name

	for evar, val := range r.Env {
		if c.Env[evar], err = regexp.Compile(val); err != nil {
			return
		}
	}

	c.Run = r.Run

	if r.Delay != "" {
		if c.Delay, err = time.ParseDuration(r.Delay); err != nil {
			return
		}
	}

	c.NoTimeout = r.NoTimeout

	return
}

type cRule struct {
	lastRun time.Time

	Name      string
	Env       map[string]*regexp.Regexp
	Run       string
	Delay     time.Duration
	NoTimeout bool
}

func (r *cRule) Match(e Event) (b bool) {
	b = true
	for evar, val := range r.Env {
		b = b && (val.MatchString(e[evar]))
	}
	return
}

func (r *cRule) MustRun() bool {
	return r.Run != "" && time.Since(r.lastRun) > time.Second*10
}

func (r *cRule) RunCommand() {
	go func() {
		time.Sleep(r.Delay)
		if r.MustRun() {
			var cmd *exec.Cmd
			var ctx context.Context
			var cancel context.CancelFunc

			cl := strings.Split(r.Run, " ")

			if r.NoTimeout {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second*30)
			}

			if len(cl) > 1 {
				cmd = exec.CommandContext(ctx, cl[0], cl[1:]...)
			} else {
				cmd = exec.CommandContext(ctx, cl[0])
			}

			log.Printf("Executing: %s after %s delay", r.Run, r.Delay)
			if output, err := cmd.CombinedOutput(); err == nil {
				r.lastRun = time.Now()
			} else {
				log.Printf("Error executing command: %s", err)
				log.Printf("\tcommand-output:\n\t%s", output)
			}

			// calling cancel function
			cancel()

		}
	}()
}

type Event map[string]string

func udevmon() (ce chan Event) {
	ce = make(chan Event)

	cmd := exec.Command("udevadm", "monitor", "--udev", "--environment")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to connect to udevadm pipe: %s", err)
	}

	if err = cmd.Start(); err != nil {
		log.Fatalf("Failed to run udevadm: %s", err)
	}

	go func() {
		defer close(ce)
		scanner := bufio.NewScanner(stdout)
		entry := make(Event)
		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				ce <- entry
				entry = make(Event)
				continue
			}

			if reVar.MatchString(line) {
				lsp := strings.SplitN(line, "=", 2)
				entry[lsp[0]] = lsp[1]
			}
		}
	}()

	return
}

type Uudev struct {
	Rules []*cRule
	Debug bool
}

func (u *Uudev) LoadRules(r io.Reader) (err error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	rules := make([]Rule, 0)

	// we decode all rules
	for {
		var rule Rule
		err = dec.Decode(&rule)
		if errors.Is(err, io.EOF) {
			err = nil
			break
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return
		}

		rules = append(rules, rule)
	}

	for _, r := range rules {
		var cr *cRule
		if cr, err = r.Compile(); err != nil {
			return
		}
		u.Rules = append(u.Rules, cr)
	}

	return
}

func (u *Uudev) Run() {
	for event := range udevmon() {
		for _, r := range u.Rules {
			if r.Match(event) {

				if u.Debug {
					b, _ := json.MarshalIndent(event, "", "    ")
					log.Printf("Rule %s matched udev event:\n%s\n", r.Name, string(b))
				}

				r.RunCommand()
			}
		}
	}
}

func main() {
	var (
		debug    bool
		force    bool
		monitor  bool
		template bool
	)

	flag.BoolVar(&debug, "d", debug, "Enable debug mode")
	flag.BoolVar(&force, "f", force, "Force uudev to start by killing old instance")
	flag.BoolVar(&template, "t", template, "Prints out a hook template")
	flag.BoolVar(&monitor, "monitor", monitor, "Monitor udev events and print them to stdout")

	flag.Parse()

	// monitoring mode
	if monitor {
		for event := range udevmon() {
			b, _ := json.MarshalIndent(event, "", "    ")
			fmt.Println(string(b))
		}
		os.Exit(0)
	}

	if template {
		r := Rule{
			Name:  "Hook Template",
			Run:   "/usr/bin/true",
			Delay: defaultDelay.String(),
		}
		if b, err := yaml.Marshal(&r); err != nil {
		} else {
			fmt.Println(string(b))
		}
		os.Exit(0)
	}

	if data, err := os.ReadFile(uudevPid); err == nil {
		if force {
			if pid, err := strconv.ParseInt(string(data), 0, 64); err == nil {
				p, _ := os.FindProcess(int(pid))
				if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					log.Fatalf("failed to kill running instance: %s", err)
				}
			}
		} else {
			log.Fatalf("uudev instance is already running pid=%s", string(data))
		}
	}

	os.MkdirAll(uudevRun, 0700)
	if err := os.WriteFile(uudevPid, []byte(fmt.Sprintf("%d", os.Getpid())), 0700); err != nil {
		log.Fatalf("failed to write pid to file %s: %s", uudevPid, err)
	}

	// register a signal handler
	signals := make(chan os.Signal, 42)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		os.Remove(uudevPid)
	}()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Cannot find user home directory: %s", err)
	}

	confPath := filepath.Join(home, ".config", "uudev", "config.yaml")
	fd, err := os.Open(confPath)
	if err != nil {
		log.Fatalf("Config file not found (%s): %s", confPath, err)
	}

	uudev := Uudev{}
	uudev.Debug = debug
	log.Printf("Loading rules: %s\n", confPath)
	if err := uudev.LoadRules(fd); err != nil {
		log.Fatalf("Failed to load uudev configuration: %s", err)
	}

	for _, rule := range uudev.Rules {
		log.Printf(`Rule loaded: name="%s" run="%s" delay="%s"`, rule.Name, rule.Run, rule.Delay)
	}

	log.Println("Monitoring new udev events")
	uudev.Run()
}
