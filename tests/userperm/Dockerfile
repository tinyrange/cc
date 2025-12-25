FROM alpine:latest

# Install sudo
RUN apk add --no-cache sudo

# Create a user
RUN adduser -S -G users -h /home/user -s /bin/sh user
RUN chown -R user:users /home/user

# Create a user entry script
RUN cat > /bin/userentry.sh <<'EOF'
#!/bin/sh

echo "In userentry.sh"

echo "User: $(whoami)"

cd ~
echo "Current directory: $(pwd)"
ls -la
echo "Hello" > ~/hello.txt

EOF
RUN chmod +x /bin/userentry.sh

# Create a entrypoint script
RUN cat > /bin/entrypoint.sh <<'EOF'
#!/bin/sh

echo "In entrypoint.sh"

echo "User: $(whoami)"

# Use sudo to switch to the user and run /bin/userentry.sh
sudo -u user /bin/userentry.sh

EOF
RUN chmod +x /bin/entrypoint.sh

ENTRYPOINT [ "/bin/entrypoint.sh" ]