FROM alpine:3.23

RUN apk add --no-cache fio

WORKDIR /benchmark

# Default benchmark script
COPY <<'EOF' /benchmark/run-benchmark.sh
#!/bin/sh
set -e

TEST_DIR=${TEST_DIR:-/benchmark/data}
SIZE=${SIZE:-1G}
RUNTIME=${RUNTIME:-10}
NUMJOBS=${NUMJOBS:-1}

mkdir -p "$TEST_DIR"

echo "=== Sequential Write Test ==="
fio --name=seq-write --directory="$TEST_DIR" --size="$SIZE" --bs=1M \
    --rw=write --ioengine=libaio --direct=1 --numjobs="$NUMJOBS" \
    --runtime="$RUNTIME" --group_reporting --time_based

echo "=== Sequential Read Test ==="
fio --name=seq-read --directory="$TEST_DIR" --size="$SIZE" --bs=1M \
    --rw=read --ioengine=libaio --direct=1 --numjobs="$NUMJOBS" \
    --runtime="$RUNTIME" --group_reporting --time_based

echo "=== Random Write Test (1M) ==="
fio --name=rand-write --directory="$TEST_DIR" --size="$SIZE" --bs=1M \
    --rw=randwrite --ioengine=libaio --direct=1 --numjobs="$NUMJOBS" \
    --runtime="$RUNTIME" --group_reporting --time_based

echo "=== Random Read Test (1M) ==="
fio --name=rand-read --directory="$TEST_DIR" --size="$SIZE" --bs=1M \
    --rw=randread --ioengine=libaio --direct=1 --numjobs="$NUMJOBS" \
    --runtime="$RUNTIME" --group_reporting --time_based

rm -rf "$TEST_DIR"/*
EOF

RUN chmod +x /benchmark/run-benchmark.sh

ENTRYPOINT ["/benchmark/run-benchmark.sh"]