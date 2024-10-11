package loadgen

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"go.uber.org/ratelimit"
)

// LimiterConfig is used to create a limiter.
type LimiterConfig struct {
	Endpoint     connection.Endpoint `mapstructure:"endpoint"`
	InitialLimit int                 `mapstructure:"initial-limit"`
}

type limiterHolder struct {
	limiter ratelimit.Limiter
}

func (h *limiterHolder) Take() time.Time {
	return h.limiter.Take()
}

func getLimiter(limit int) ratelimit.Limiter {
	if limit < 1 {
		logger.Infof("Setting to unlimited (value passed: %d).", limit)
		return ratelimit.NewUnlimited()
	} else {
		logger.Infof("Setting limit to %d requests per second.", limit)
		// create our new limiter
		return ratelimit.New(limit)
	}
}

// NewLimiter instantiate a new rate limiter with optional remote control capabilities.
func NewLimiter(c *LimiterConfig) ratelimit.Limiter {
	if c == nil {
		return ratelimit.NewUnlimited()
	}

	initialLimiter := getLimiter(c.InitialLimit)

	// Allow fixed limit.
	if c.Endpoint.Empty() {
		return initialLimiter
	}

	rl := limiterHolder{limiter: initialLimiter}

	// start remote-limiter controller.
	logger.Infof("Start remote controller listener on %s\n", c.Endpoint.Address())
	logger.Infof("Serving...")
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.POST("/setLimits", func(c *gin.Context) {
		logger.Infof("Received limit request.")

		type LimitRequest struct {
			Limit int `json:"limit"`
		}

		var request LimitRequest
		if err := c.BindJSON(&request); err != nil {
			logger.Errorf("error deserializing request: %v", err)
		}
		logger.Infof("Setting limit to %d", request.Limit)

		rl.limiter = getLimiter(request.Limit)

		c.IndentedJSON(http.StatusOK, request)
	})
	go func() {
		err := router.Run(c.Endpoint.Address())
		if err != nil {
			logger.Errorf("Error running rate limit remote controller: %s", err)
		}
	}()

	return &rl
}
