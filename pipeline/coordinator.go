package pipeline

type coordinator struct {
	dependencyMgr   *dependencyMgr
	sigVerifierMgr  *sigVerifierMgr
	shardsServerMgr *shardsServerMgr
}

func newCordinator(c *Config) (*coordinator, error) {
	dependencyMgr := newDependencyMgr()
	sigVerifierMgr, err := newSigVerificationMgr(c.SigVerifierMgrConfig)
	if err != nil {
		return nil, err
	}
	shardsServerMgr, err := newShardsServerMgr(c.ShardsServerMgrConfig)
	if err != nil {
		return nil, err
	}
	return &coordinator{
		dependencyMgr:   dependencyMgr,
		sigVerifierMgr:  sigVerifierMgr,
		shardsServerMgr: shardsServerMgr,
	}, nil
}
