package limiter

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"go.uber.org/ratelimit"
)

var logger = logging.New("ratelimiter")

type limiterHolder struct {
	limiter ratelimit.Limiter
}

func (h *limiterHolder) Take() time.Time {
	return h.limiter.Take()
}

func New(controllerEndpoint *connection.Endpoint) ratelimit.Limiter {
	var rl limiterHolder
	// we start by default with unlimited rate
	rl.limiter = ratelimit.NewUnlimited()

	if controllerEndpoint == nil || controllerEndpoint.Empty() {
		return &rl
	}

	// start remote-limiter controller
	logger.Infof("Start remote controller listener on %s\n", controllerEndpoint.Address())
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

		if limit.Limit < 1 {
			logger.Infof("Setting to unlimited (value passed: %d).", limit.Limit)
			rl.limiter = ratelimit.NewUnlimited()
			return
		}

		logger.Infof("Setting limit to %d TPS.", limit.Limit)
		// create our new limiter
		rl.limiter = ratelimit.New(limit.Limit)

		c.IndentedJSON(http.StatusOK, limit)
	})
	go router.Run(controllerEndpoint.Address())

	return &rl
}
