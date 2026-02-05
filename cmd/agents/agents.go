package main

// Agent defines a VM agent configuration.
type Agent struct {
	Name        string   // Agent identifier (e.g., "claude")
	Description string   // Human-readable description
	Dockerfile  string   // Dockerfile content to build the image
	DefaultCmd  []string // Default command when -term is not used
	MemoryMB    uint64   // Default memory (can be overridden)
}

// Registry of available agents - add new agents here
var agents = map[string]Agent{
	"claude": {
		Name:        "claude",
		Description: "Claude Code AI assistant",
		Dockerfile:  claudeDockerfile,
		DefaultCmd:  []string{"su", "agent", "sh", "-c", "/home/agent/.local/bin/claude --dangerously-skip-permissions"},
		MemoryMB:    8192,
	},
}

const claudeDockerfile = `FROM debian:bookworm-slim

# Install essential tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    procps \
	sudo \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash agent

# Add agent user to sudoers
RUN echo "agent ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers

# Install Claude Code using native installer (as agent user)
USER agent
WORKDIR /home/agent
# enable command tracing
RUN curl -fsSL https://claude.ai/install.sh | bash

ENV HOME=/home/agent
ENV PATH="/home/agent/.local/bin:${PATH}"

CMD ["/bin/bash"]
`

func getAgent(name string) (Agent, bool) {
	a, ok := agents[name]
	return a, ok
}

func listAgents() []Agent {
	var list []Agent
	for _, a := range agents {
		list = append(list, a)
	}
	return list
}
