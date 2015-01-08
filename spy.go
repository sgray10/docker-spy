package main

import (
	dockerApi "github.com/fsouza/go-dockerclient"
	"github.com/miekg/dns"
	"log"
	"strings"
)

type Container struct {
	first string
	status string
	created bool
}

type Spy struct {
	docker *dockerApi.Client
	dns    *DNS
	containers map[string]Container
}

type PublishedName struct {
  name string
	ip string
}

func (s *Spy) Watch() {
	s.registerRunningContainers()
	events := make(chan *dockerApi.APIEvents)
	s.docker.AddEventListener(events)
	go s.readEventStream(events)
}

func (s *Spy) registerRunningContainers() {
	containers, err := s.docker.ListContainers(dockerApi.ListContainersOptions{})
	if err != nil {
		log.Fatalf("Unable to register running containers: %v", err)
	}
	for _, listing := range containers {
		s.handleContainerEvent(listing.ID, listing.Status)
	}
}

func (s *Spy) readEventStream(events chan *dockerApi.APIEvents) {
	for msg := range events {
		s.handleContainerEvent(msg.ID, msg.Status)
	}
}

func (s *Spy) handleContainerEvent(id string, status string) {
	prev, exists := s.containers[id];
	first := !exists && strings.Contains(status, "create")
	created := exists && strings.Contains(prev.first, "create")
	started := created && strings.Contains(status, "start")
	register := started || strings.Contains(status, "Up")
	finished := strings.Contains(status, "die")
	unregister := finished && exists
	update := register || first

	var c = Container{status: status, created: false, first: ""}
	if first {
		c.first = status
	}
	if update {
		c.created = true
		s.containers[id] = c
	}
	if register {
		s.registerAllNames(id)
	}
	if unregister {
		s.unregisterAllNames(id)
		delete(s.containers, id)
	}
}

func (s *Spy) getContainerNames(id string) ([]PublishedName, error) {
	names := make([]PublishedName, 0)
	container, err := s.docker.InspectContainer(id)
	if err == nil {
		names = append(names, PublishedName{
			name: container.Config.Hostname + "." + container.Config.Domainname + ".",
			ip: container.NetworkSettings.IPAddress,
		})
		for _, line := range container.Config.Env {
			env := strings.SplitN(line, "=", 2)
			if strings.HasPrefix(env[0], "DNS_PUBLISH_NAME_") && len(env) > 1 {
				info := strings.SplitN(env[1], ":", 2)
				if len(info) > 1 {
					names = append(names, PublishedName{
					  name: info[0] + ".",
						ip: info[1],
					})
				}
			}
		}
	}
	return names, err
}

func (s *Spy) registerAllNames(id string) {
	names, err := s.getContainerNames(id)
	if err != nil {
		log.Printf("ERROR: %+v\n", names)
		delete(s.containers, id)
		return
	}
	for _, name := range names {
		s.registerName(name)
	}
}

func (s *Spy) unregisterAllNames(id string) {
	names, err := s.getContainerNames(id)
	if err != nil {
		log.Printf("ERROR: %+v\n", names)
		delete(s.containers, id)
		return
	}
	for _, name := range names {
		s.unregisterName(name)
	}
}

func (s *Spy) registerName(name PublishedName) {
	arpa, err := dns.ReverseAddr(name.ip)
	if err != nil {
		log.Printf("Unable to create ARPA address. Reverse DNS lookup will be unavailable for this container.")
		log.Printf("%+v\n", err)
	}
	log.Printf("Adding record: %+v", name)
	s.dns.cache.Set(name.name, &Record{
		name.ip,
		arpa,
		name.name,
	})
}

func (s *Spy) unregisterName(name PublishedName) {
	log.Printf("Removing record: %+v", name)
	s.dns.cache.Remove(name.name)
}
