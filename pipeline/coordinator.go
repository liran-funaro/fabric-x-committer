package pipeline

type coordinator struct {
	dependencyMgr   *dependencyMgr
	sigVerifierMgr  *sigVerifierMgr
	shardsServerMgr *shardsServerMgr
}
