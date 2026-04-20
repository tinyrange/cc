from __future__ import annotations

import time


def main() -> None:
    t0 = time.perf_counter()
    import neurodesk as nd

    t1 = time.perf_counter()
    print(f"import neurodesk: {t1 - t0:.3f}s")

    t2 = time.perf_counter()
    niimath = nd.container("niimath")
    t3 = time.perf_counter()
    print(f'nd.container("niimath"): {t3 - t2:.3f}s')
    print(f"container path: {niimath.path}")
    print(f"container base_url: {niimath.base_url}")

    t4 = time.perf_counter()
    out = niimath.niimath()
    t5 = time.perf_counter()
    print(f"niimath.niimath(): {t5 - t4:.3f}s")
    print("output preview:")
    print(out[:800])

    t6 = time.perf_counter()
    niimath.close()
    t7 = time.perf_counter()
    print(f"container.close(): {t7 - t6:.3f}s")
    print(f"total script time: {t7 - t0:.3f}s")


if __name__ == "__main__":
    main()
