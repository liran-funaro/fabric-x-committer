package sigverification_test

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
)

type Output = *sigverification.Response

type Stream = sigverification.Verifier_StartStreamClient

func ChannelOutputLength(ch <-chan []Output) chan int {
	output := make(chan int)
	go func() {
		for {
			response := <-ch
			if len(response) > 0 {
				output <- len(response)
			}
		}
	}()
	return output
}

func StreamOutputLength(stream sigverification.Verifier_StartStreamClient) chan int {
	output := make(chan int)
	go func() {
		for {
			response, _ := stream.Recv()
			if response == nil || response.Responses == nil {
				return
			}
			if len(response.Responses) > 0 {
				output <- len(response.Responses)
			}
		}
	}()
	return output
}

func OutputChannel(stream sigverification.Verifier_StartStreamClient) <-chan []*sigverification.Response {
	output := make(chan []*sigverification.Response)
	go func() {
		for {
			response, _ := stream.Recv()
			if response == nil || response.Responses == nil {
				return
			}
			if len(response.Responses) > 0 {
				output <- response.Responses
			}
		}
	}()
	return output
}
