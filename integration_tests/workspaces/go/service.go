package main

import "fmt"

// ServiceImpl is a second implementation of SharedInterface
type ServiceImpl struct {
	ServiceName string
	Active      bool
}

// Process implements SharedInterface for ServiceImpl
func (s *ServiceImpl) Process() error {
	if !s.Active {
		return fmt.Errorf("service %s is not active", s.ServiceName)
	}
	fmt.Printf("Service %s processing\n", s.ServiceName)
	return nil
}

// GetName implements SharedInterface for ServiceImpl
func (s *ServiceImpl) GetName() string {
	return s.ServiceName
}

// RunService creates and uses a ServiceImpl via the SharedInterface
func RunService() {
	svc := &ServiceImpl{ServiceName: "test-service", Active: true}
	var iface SharedInterface = svc
	iface.Process()
	fmt.Println(iface.GetName())
}
