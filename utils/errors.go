package utils

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
)

type serviceErrors struct {
	ch          <-chan error
	numServices int
	description string
}

func (e *serviceErrors) writeTo(errChan chan<- error) {
	go func() {
		for i := 0; i < e.numServices; i++ {
			err := <-e.ch
			if err != nil {
				errChan <- errors.Wrapf(err, "error occurred: %s", e.description)
				break
			}
		}
	}()
}

func ServiceErrors(ch <-chan error, numServices int, description string) *serviceErrors {
	return &serviceErrors{
		ch:          ch,
		numServices: numServices,
		description: description,
	}
}

func Capture(services ...*serviceErrors) <-chan error {
	errChan := make(chan error)
	for _, serviceErrs := range services {
		serviceErrs.writeTo(errChan)
	}
	return errChan
}

func RegisterInterrupt(cancel context.CancelFunc) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
		os.Exit(1)
	}()
}

func ErrorUnlessStopped(stop <-chan any, err error) error {
	select {
	case <-stop:
		return nil
	default:
		return err
	}
}
