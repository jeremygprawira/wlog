// This file holds the vocabulary an audit record uses: who acted, and how it ended.
package audit

// Actor types. Every record names one, so a reader can filter by who acted without
// guessing from the id.
const (
	// ActorUser is a person, such as the customer or the operator.
	ActorUser = "user"
	// ActorService is another service acting on its own behalf.
	ActorService = "service"
	// ActorSystem is wlog itself or a scheduled process with no person behind it.
	ActorSystem = "system"
	// ActorAgent is an AI agent. It also records its model, its tools, and the prompt it
	// ran, so a review can tell which agent generation did what.
	ActorAgent = "agent"
)

// Outcomes. A record always ends in one of these three.
const (
	// OutcomeSuccess is an action that completed as asked.
	OutcomeSuccess = "success"
	// OutcomeDenied is an action a policy refused.
	OutcomeDenied = "denied"
	// OutcomeFailure is an action that was allowed and then failed.
	OutcomeFailure = "failure"
)

// actorTypes is the set Valid accepts.
var actorTypes = map[string]bool{
	ActorUser:    true,
	ActorService: true,
	ActorSystem:  true,
	ActorAgent:   true,
}

// Valid reports whether the actor names one of the four types. An empty type counts as
// valid, because a caller who does not care about the type should not be forced to pick
// one; an unknown type is a typo worth catching in a test.
func (a Actor) Valid() bool {
	return a.Type == "" || actorTypes[a.Type]
}
