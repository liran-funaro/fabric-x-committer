package loadgen

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"go.uber.org/ratelimit"
)

var NoLimit = LimiterConfig{
	Endpoint:     connection.Endpoint{},
	InitialLimit: -1,
}

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
		logger.Infof("Setting limit to %d blocks per second.", limit)
		// create our new limiter
		return ratelimit.New(limit)
	}
}

func NewLimiter(c *LimiterConfig) ratelimit.Limiter {
	if c == nil || c.Endpoint.Empty() {
		return ratelimit.NewUnlimited()
	}
	rl := limiterHolder{limiter: getLimiter(c.InitialLimit)}

	// start remote-limiter controller
	logger.Infof("Start remote controller listener on %s\n", c.Endpoint.Address())
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
	go router.Run(c.Endpoint.Address())

	return &rl
}
