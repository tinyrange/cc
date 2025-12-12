• Made HPET actually tick and advertise capabilities, plus added targeted logging:

  - HPET now maintains its counter via a background ticker, supports periodic re‑arm, preserves capability bits/route masks, and logs config/comparator/IRQ events.
  - Corrected the ACPI HPET table ID fields (revision/comparator count/vendor, 64‑bit counter) and kept legacy capability advertised.

Ran ./tools/build.go -run -- alpine whoami (and with -dmesg); the guest still times out in setup_boot_APIC_clock, PIT channel 0 is never programmed (only ch2 polls), IOAPIC entries stay masked, and no SetIRQ logs fire. Notably, the new HPET logs never appear, so the kernel still isn’t touching the HPET MMIO registers.

Next steps I’d take:

1. Verify the ACPI HPET table the guest sees (dump/inspect the built tables) to confirm it’s valid and reachable.
2. Track why the HPET driver isn’t probing—e.g., ensure the kernel config enables HPET or adjust the table/flags if needed.
3. If HPET remains unavailable, consider a fallback to supply a periodic tick (e.g., PIT ch0) so jiffies advance and APIC timer calibration can proceed.