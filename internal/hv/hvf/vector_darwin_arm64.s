#include "textflag.h"

TEXT vectorTrampoline<>(SB), NOSPLIT|NOFRAME, $0
    VLD1 (R2), [V0.B16]
    JMP (R3)

TEXT ·vectorTrampolineAddr(SB), NOSPLIT, $0-8
    MOVD $vectorTrampoline<>(SB), R0
    MOVD R0, ret+0(FP)
    RET
