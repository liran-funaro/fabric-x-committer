package crypto

import (
	"context"
	"fmt"
	"testing"
)

var r []byte
var rerr error

func BenchmarkSign(b *testing.B) {
	msg := []byte("Hello World")
	sk, _ := NewECDSAKey()

	var sig []byte
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		sig, _ = SignMessage(sk, msg)
	}
	r = sig
}

func BenchmarkSignP(b *testing.B) {
	msg := []byte("Hello World")
	sk, _ := NewECDSAKey()

	// note that in this test, signatures are short-lived objects and GC may impact the performance due to heaving cleaning
	// Since we have loads of memory, GC can be reduced and therefore keep more CPU cycles for signing
	// use GOGC=20000 as a good value on our 20core test servers

	for _, numWorkers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("worker-%d", numWorkers), func(b *testing.B) {
			sigs := make(chan []byte, 10000)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			for i := 0; i < numWorkers; i++ {
				go func(ctx context.Context, i int) {

					for {
						select {
						case <-ctx.Done():
							return
						default:
							s, _ := SignMessage(sk, msg)
							sigs <- s
						}
					}
				}(ctx, i)
			}

			var sig []byte
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				sig = <-sigs
			}
			r = sig

		})
	}

}

func BenchmarkVerify(b *testing.B) {
	msg := []byte("Hello World")
	sk, _ := NewECDSAKey()
	sig, _ := SignMessage(sk, msg)

	var err error
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		err = VerifyMessage(&sk.PublicKey, msg, sig)
	}
	rerr = err
}
