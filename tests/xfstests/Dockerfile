FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install required packages
RUN apt-get update && apt-get install -y \
    acl \
    attr \
    automake \
    bc \
    dbench \
    dump \
    e2fsprogs \
    fio \
    gawk \
    gcc \
    git \
    indent \
    libacl1-dev \
    libaio-dev \
    libcap-dev \
    libgdbm-dev \
    libgdbm-compat-dev \
    libtool \
    libtool-bin \
    liburing-dev \
    libuuid1 \
    lvm2 \
    make \
    psmisc \
    python3 \
    quota \
    sed \
    uuid-dev \
    uuid-runtime \
    xfsprogs \
    xfslibs-dev \
    sqlite3 \
    pkg-config \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Create test users and groups
RUN useradd -m fsgqa && \
    useradd fsgqa2 && \
    groupadd fsgqa || true

# Clone and build xfstests
WORKDIR /opt
RUN git clone git://git.kernel.org/pub/scm/fs/xfs/xfstests-dev.git xfstests && \
    cd xfstests && \
    make && \
    make install

WORKDIR /opt/xfstests

# Create scratch directory
RUN mkdir -p /scratch /mnt

# Create local.config for virtio-fs testing
# TEST_DIR is the virtio-fs mount at /mnt
# SCRATCH_MNT is a subdirectory for scratch operations
RUN cat > /opt/xfstests/local.config <<EOF
export FSTYP=virtiofs
export TEST_DEV=testfs
export TEST_DIR=/mnt
export SCRATCH_DEV=scratchfs
export SCRATCH_MNT=/scratch
EOF

# Entry point script
RUN cat > /opt/xfstests/run-tests.sh <<EOF
#!/bin/bash
set -e

cd /opt/xfstests

# Run tests - pass any arguments to check
if [ $# -eq 0 ]; then
    # Default: run generic quick tests suitable for virtiofs
    exec ./check -g quick
else
    exec ./check "$@"
fi
EOF

RUN chmod +x /opt/xfstests/run-tests.sh

ENTRYPOINT ["/opt/xfstests/run-tests.sh"]
CMD []