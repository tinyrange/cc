#include "textflag.h"

TEXT ·hostCounterFrequency(SB), NOSPLIT, $0-8
    MRS CNTFRQ_EL0, R0
    MOVD R0, ret+0(FP)
    RET
