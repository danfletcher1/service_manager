package srvmgr

/*

Credits
=======

Description
===========
Service Manager reliably runs your code without the worry of it crashing.
All Kernel panics are handled, outputing a debug message and then restarting.
This is designed for continious running applications or services.

There is a function

Use
===

func main() {

	service, _ := srv.New(srv.Timeout(time.Second * 10))
	
	defer service.TerminationHandling()

	// running a continious loop
	service.RunFunction("test function", func(ctx context.Context) {
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			default:
				<-time.After(time.Second * 4)
				fmt.Printf("A")
			}
		}
	})

	// running a timed service loop
	service.RunFunction("test function", func(ctx context.Context) {
	interval := time.Ticker(time.Second * 1)
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			case <-interval.C:
				fmt.Printf("B")
			}
		}
		interval.Stop()
	})

	// running single execution function, use the cancel context where possible to wrapup
	service.RunFunction("test function", func(ctx context.Context) {
		<-time.After(time.Second * 2)
		fmt.Println("C")
	})

	// You don't need to worry about termination of the program, termination handling will wait
	// Until all the functions have executed.
}

*/

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"time"
)

// Config requires no user values but hold important unexported information
type Config struct {
	cancel  context.CancelFunc
	ctx     context.Context
	running int
	exit    chan error
	timeout time.Duration // Max wait time for save shutdown after CTRL-C
}

// Option belongs to the new config creation
type option func(*Config) error

// New returns a new config struct.
// Listens for CTRL-C
func New(options ...option) (*Config, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Default configuration
	newConfig := &Config{
		cancel:  cancel,
		ctx:     ctx,
		exit:    make(chan error, 1),
		timeout: time.Second * 60,
	}

	// Update default config with any options received
	for _, x := range options {
		err := x(newConfig)
		if err != nil {
			return nil, err
		}
	}

	// Handle CTRL-C and context close signals
	go newConfig.closeHandling()

	return newConfig, nil
}

// AddContext that already exists for nexting closure
func AddContext(ctx context.Context) option {
	return func(c *Config) error {
		c.ctx, c.cancel = context.WithCancel(ctx)
		return nil
	}
}

// Timeout is a new configuration option.
// Set the max time to wait, before forcing a function to close.
func Timeout(t time.Duration) option {
	return func(c *Config) error {
		c.timeout = t
		return nil
	}
}

// Signal for the everything to quit nicely
func (c *Config) Quit() {
	c.cancel()
}

// Close handling deals with the nice ordered shutdown of the system
func (c *Config) closeHandling() {
	// Create the CTRL-C Signal channel
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)

	// Create the context channel
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	// Wait for either CTRL-L or context cancel signal
	select {
	case <-ch:
		c.cancel()
		fmt.Println("[CTRL-C received, starting shutdown]")
	case <-ctx.Done():
		fmt.Println("[Quit received, starting shutdown]")
	}

	// Start a close timer
	<-time.After(c.timeout)

	// If timer has expired send exit signal with error
	c.exit <- fmt.Errorf("[Error shutdown timeout expired, dirty exit]")
}

// TerminationHandling is to be used after initial config
// It will handling the main function ending nicely. Terminating all the started functions
func (c *Config) TerminationHandling() {
	// This will stop it shutting down until shutdown procedure has completed
	if err := <-c.exit; err != nil {
		fmt.Println(err)
	}
}

// RunFunction will execute a blocking function reliably, restarting the function on a panic
// If you want a looping function just incorporate that
func (c *Config) RunFunction(srv string, f func(context.Context)) {
	c.running++
	fmt.Printf("[%v Started]\n", srv)
	// The first go is to run as a background task
	go func(srv string, c *Config, f func(context.Context)) {
		defer func(c *Config) {
			fmt.Printf("[%v Stopped]\n", srv)
			c.running--
			if c.running == 0 {
				close(c.exit)
			}
		}(c)

		wait := make(chan struct{}, 1)

		// This is the context for the running function
		ctx, cancel := context.WithCancel(c.ctx)
		defer cancel()

		// Run in a loop until cancel() is called
		for ctx.Err() == nil {
			// It runs in a go routine to keep the panic within the loop
			go func(srv string, ctx context.Context, cancel context.CancelFunc) {
				// defer loads in reverse order so wait will continue after panic text
				defer func() { wait <- struct{}{} }()
				// output recover info
				defer panicRecovery(srv, "Panic, restarting in 5s")

				// This should be a blocking function
				f(ctx)

				// Cancel will run on normal termination
				cancel()
			}(srv, ctx, cancel)
			<-wait
		}
	}(srv, c, f)
}

func panicRecovery(i ...interface{}) {
	if r := recover(); r != nil {
		fmt.Println(r)
		fmt.Println(i)
		debug.PrintStack()
		time.Sleep(time.Second * 5)
	}
}
