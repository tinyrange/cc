FROM alpine:latest

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Create a test user
RUN adduser -D -u 1000 testuser

# Create a C program that prints real and effective UID
RUN cat > /tmp/checkuid.c <<'EOF'
#include <stdio.h>
#include <unistd.h>
#include <sys/types.h>

int main() {
    uid_t ruid = getuid();
    uid_t euid = geteuid();

    printf("Real UID: %d\n", ruid);
    printf("Effective UID: %d\n", euid);

    if (ruid != 0 && euid == 0) {
        printf("SUCCESS: setuid is working correctly\n");
        return 0;
    } else if (ruid == 0) {
        printf("ERROR: Running as root, cannot test setuid\n");
        return 1;
    } else {
        printf("FAILURE: setuid not working (euid should be 0)\n");
        return 1;
    }
}
EOF

# Compile and set setuid bit
RUN gcc -o /usr/local/bin/checkuid /tmp/checkuid.c && \
    chown root:root /usr/local/bin/checkuid && \
    chmod 4755 /usr/local/bin/checkuid

# Show the permissions
RUN ls -la /usr/local/bin/checkuid

# Create entrypoint that runs as testuser
RUN cat > /entrypoint.sh <<'EOF'
#!/bin/sh
set -e

echo "=== Setuid Test ==="
echo "Running checkuid as testuser..."
echo ""

# Run the setuid binary as testuser
su testuser -c /usr/local/bin/checkuid

echo ""
echo "=== Test Complete ==="
EOF
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
