package main

type AccessView struct {
	CanDomainAdmin bool
	CanBoxAdmin    bool
}

func accessView(s *Session) AccessView {
	if s == nil {
		return AccessView{}
	}
	return AccessView{
		CanDomainAdmin: isDomainAdmin(s.User, s.Domain),
		CanBoxAdmin:    isBoxAdmin(s.User, s.Domain),
	}
}
