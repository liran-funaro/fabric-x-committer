package limiter

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"go.uber.org/ratelimit"
)

var logger = logging.New("ratelimiter")

type Config struct {
	Endpoint     connection.Endpoint `mapstructure:"endpoint"`
	InitialLimit int                 `mapstructure:"initial-limit"`
}

type LimiterSetter interface {
	ratelimit.Limiter
	Set(int)
}

type limiterHolder struct {
	limiter ratelimit.Limiter
}

func (h *limiterHolder) Take() time.Time {
	return h.limiter.Take()
}
func (h *limiterHolder) Set(limit int) {
	if limit < 1 {
		logger.Infof("Setting to unlimited (value passed: %d).", limit)
		h.limiter = ratelimit.NewUnlimited()
	} else {
		logger.Infof("Setting limit to %d TPS.", limit)
		// create our new limiter
		h.limiter = ratelimit.New(limit)
	}
}

func New(c *Config) LimiterSetter {
	var rl limiterHolder
	if c == nil || c.Endpoint.Empty() {
		return &rl
	}
	rl.Set(c.InitialLimit)

	// start remote-limiter controller
	logger.Infof("Start remote controller listener on %s\n", c.Endpoint.Address())
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.POST("/setLimits", func(c *gin.Context) {
		logger.Infof("Received limit request.")

		type Limiter struct {
			Limit int `json:"limit"`
		}

		var limit Limiter
		if err := c.BindJSON(&limit); err != nil {
			logger.Errorf("error deserializing request: %v", err)
		}

		rl.Set(limit.Limit)

		c.IndentedJSON(http.StatusOK, limit)
	})
	go router.Run(c.Endpoint.Address())

	return &rl
}
