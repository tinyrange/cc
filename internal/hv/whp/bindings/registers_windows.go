//go:build windows

package bindings

import "fmt"

// RegisterName mirrors WHV_REGISTER_NAME.
type RegisterName uint32

// X64 General Purpose Registers
const (
	RegisterRax    RegisterName = 0x00000000
	RegisterRcx    RegisterName = 0x00000001
	RegisterRdx    RegisterName = 0x00000002
	RegisterRbx    RegisterName = 0x00000003
	RegisterRsp    RegisterName = 0x00000004
	RegisterRbp    RegisterName = 0x00000005
	RegisterRsi    RegisterName = 0x00000006
	RegisterRdi    RegisterName = 0x00000007
	RegisterR8     RegisterName = 0x00000008
	RegisterR9     RegisterName = 0x00000009
	RegisterR10    RegisterName = 0x0000000A
	RegisterR11    RegisterName = 0x0000000B
	RegisterR12    RegisterName = 0x0000000C
	RegisterR13    RegisterName = 0x0000000D
	RegisterR14    RegisterName = 0x0000000E
	RegisterR15    RegisterName = 0x0000000F
	RegisterRip    RegisterName = 0x00000010
	RegisterRflags RegisterName = 0x00000011
)

// X64 Segment Registers
const (
	RegisterEs   RegisterName = 0x00000012
	RegisterCs   RegisterName = 0x00000013
	RegisterSs   RegisterName = 0x00000014
	RegisterDs   RegisterName = 0x00000015
	RegisterFs   RegisterName = 0x00000016
	RegisterGs   RegisterName = 0x00000017
	RegisterLdtr RegisterName = 0x00000018
	RegisterTr   RegisterName = 0x00000019
)

// X64 Table Registers
const (
	RegisterIdtr RegisterName = 0x0000001A
	RegisterGdtr RegisterName = 0x0000001B
)

// X64 Control Registers
const (
	RegisterCr0 RegisterName = 0x0000001C
	RegisterCr2 RegisterName = 0x0000001D
	RegisterCr3 RegisterName = 0x0000001E
	RegisterCr4 RegisterName = 0x0000001F
	RegisterCr8 RegisterName = 0x00000020
)

// X64 Debug Registers
const (
	RegisterDr0 RegisterName = 0x00000021
	RegisterDr1 RegisterName = 0x00000022
	RegisterDr2 RegisterName = 0x00000023
	RegisterDr3 RegisterName = 0x00000024
	RegisterDr6 RegisterName = 0x00000025
	RegisterDr7 RegisterName = 0x00000026
)

// X64 Extended Control Registers
const (
	RegisterXCr0 RegisterName = 0x00000027
)

// X64 Virtual Control Registers
const (
	RegisterVirtualCr0 RegisterName = 0x00000028
	RegisterVirtualCr3 RegisterName = 0x00000029
	RegisterVirtualCr4 RegisterName = 0x0000002A
	RegisterVirtualCr8 RegisterName = 0x0000002B
)

// X64 Floating Point and Vector Registers
const (
	RegisterXmm0             RegisterName = 0x00001000
	RegisterXmm1             RegisterName = 0x00001001
	RegisterXmm2             RegisterName = 0x00001002
	RegisterXmm3             RegisterName = 0x00001003
	RegisterXmm4             RegisterName = 0x00001004
	RegisterXmm5             RegisterName = 0x00001005
	RegisterXmm6             RegisterName = 0x00001006
	RegisterXmm7             RegisterName = 0x00001007
	RegisterXmm8             RegisterName = 0x00001008
	RegisterXmm9             RegisterName = 0x00001009
	RegisterXmm10            RegisterName = 0x0000100A
	RegisterXmm11            RegisterName = 0x0000100B
	RegisterXmm12            RegisterName = 0x0000100C
	RegisterXmm13            RegisterName = 0x0000100D
	RegisterXmm14            RegisterName = 0x0000100E
	RegisterXmm15            RegisterName = 0x0000100F
	RegisterFpMmx0           RegisterName = 0x00001010
	RegisterFpMmx1           RegisterName = 0x00001011
	RegisterFpMmx2           RegisterName = 0x00001012
	RegisterFpMmx3           RegisterName = 0x00001013
	RegisterFpMmx4           RegisterName = 0x00001014
	RegisterFpMmx5           RegisterName = 0x00001015
	RegisterFpMmx6           RegisterName = 0x00001016
	RegisterFpMmx7           RegisterName = 0x00001017
	RegisterFpControlStatus  RegisterName = 0x00001018
	RegisterXmmControlStatus RegisterName = 0x00001019
)

// X64 MSRs
const (
	RegisterTsc                   RegisterName = 0x00002000
	RegisterEfer                  RegisterName = 0x00002001
	RegisterKernelGsBase          RegisterName = 0x00002002
	RegisterApicBase              RegisterName = 0x00002003
	RegisterPat                   RegisterName = 0x00002004
	RegisterSysenterCs            RegisterName = 0x00002005
	RegisterSysenterEip           RegisterName = 0x00002006
	RegisterSysenterEsp           RegisterName = 0x00002007
	RegisterStar                  RegisterName = 0x00002008
	RegisterLstar                 RegisterName = 0x00002009
	RegisterCstar                 RegisterName = 0x0000200A
	RegisterSfmask                RegisterName = 0x0000200B
	RegisterInitialApicID         RegisterName = 0x0000200C
	RegisterMsrMtrrCap            RegisterName = 0x0000200D
	RegisterMsrMtrrDefType        RegisterName = 0x0000200E
	RegisterMsrMtrrPhysBase0      RegisterName = 0x00002010
	RegisterMsrMtrrPhysBase1      RegisterName = 0x00002011
	RegisterMsrMtrrPhysBase2      RegisterName = 0x00002012
	RegisterMsrMtrrPhysBase3      RegisterName = 0x00002013
	RegisterMsrMtrrPhysBase4      RegisterName = 0x00002014
	RegisterMsrMtrrPhysBase5      RegisterName = 0x00002015
	RegisterMsrMtrrPhysBase6      RegisterName = 0x00002016
	RegisterMsrMtrrPhysBase7      RegisterName = 0x00002017
	RegisterMsrMtrrPhysBase8      RegisterName = 0x00002018
	RegisterMsrMtrrPhysBase9      RegisterName = 0x00002019
	RegisterMsrMtrrPhysBaseA      RegisterName = 0x0000201A
	RegisterMsrMtrrPhysBaseB      RegisterName = 0x0000201B
	RegisterMsrMtrrPhysBaseC      RegisterName = 0x0000201C
	RegisterMsrMtrrPhysBaseD      RegisterName = 0x0000201D
	RegisterMsrMtrrPhysBaseE      RegisterName = 0x0000201E
	RegisterMsrMtrrPhysBaseF      RegisterName = 0x0000201F
	RegisterMsrMtrrPhysMask0      RegisterName = 0x00002040
	RegisterMsrMtrrPhysMask1      RegisterName = 0x00002041
	RegisterMsrMtrrPhysMask2      RegisterName = 0x00002042
	RegisterMsrMtrrPhysMask3      RegisterName = 0x00002043
	RegisterMsrMtrrPhysMask4      RegisterName = 0x00002044
	RegisterMsrMtrrPhysMask5      RegisterName = 0x00002045
	RegisterMsrMtrrPhysMask6      RegisterName = 0x00002046
	RegisterMsrMtrrPhysMask7      RegisterName = 0x00002047
	RegisterMsrMtrrPhysMask8      RegisterName = 0x00002048
	RegisterMsrMtrrPhysMask9      RegisterName = 0x00002049
	RegisterMsrMtrrPhysMaskA      RegisterName = 0x0000204A
	RegisterMsrMtrrPhysMaskB      RegisterName = 0x0000204B
	RegisterMsrMtrrPhysMaskC      RegisterName = 0x0000204C
	RegisterMsrMtrrPhysMaskD      RegisterName = 0x0000204D
	RegisterMsrMtrrPhysMaskE      RegisterName = 0x0000204E
	RegisterMsrMtrrPhysMaskF      RegisterName = 0x0000204F
	RegisterMsrMtrrFix64k00000    RegisterName = 0x00002070
	RegisterMsrMtrrFix16k80000    RegisterName = 0x00002071
	RegisterMsrMtrrFix16kA0000    RegisterName = 0x00002072
	RegisterMsrMtrrFix4kC0000     RegisterName = 0x00002073
	RegisterMsrMtrrFix4kC8000     RegisterName = 0x00002074
	RegisterMsrMtrrFix4kD0000     RegisterName = 0x00002075
	RegisterMsrMtrrFix4kD8000     RegisterName = 0x00002076
	RegisterMsrMtrrFix4kE0000     RegisterName = 0x00002077
	RegisterMsrMtrrFix4kE8000     RegisterName = 0x00002078
	RegisterMsrMtrrFix4kF0000     RegisterName = 0x00002079
	RegisterMsrMtrrFix4kF8000     RegisterName = 0x0000207A
	RegisterTscAux                RegisterName = 0x0000207B
	RegisterBndcfgs               RegisterName = 0x0000207C
	RegisterMCount                RegisterName = 0x0000207E
	RegisterACount                RegisterName = 0x0000207F
	RegisterSpecCtrl              RegisterName = 0x00002084
	RegisterPredCmd               RegisterName = 0x00002085
	RegisterTscVirtualOffset      RegisterName = 0x00002087
	RegisterTsxCtrl               RegisterName = 0x00002088
	RegisterXss                   RegisterName = 0x0000208B
	RegisterUCet                  RegisterName = 0x0000208C
	RegisterSCet                  RegisterName = 0x0000208D
	RegisterSsp                   RegisterName = 0x0000208E
	RegisterPl0Ssp                RegisterName = 0x0000208F
	RegisterPl1Ssp                RegisterName = 0x00002090
	RegisterPl2Ssp                RegisterName = 0x00002091
	RegisterPl3Ssp                RegisterName = 0x00002092
	RegisterInterruptSspTableAddr RegisterName = 0x00002093
	RegisterTscDeadline           RegisterName = 0x00002095
	RegisterTscAdjust             RegisterName = 0x00002096
	RegisterUmwaitControl         RegisterName = 0x00002098
	RegisterXfd                   RegisterName = 0x00002099
	RegisterXfdErr                RegisterName = 0x0000209A
)

// X64 Feature Control and Nested Capability MSRs
const (
	RegisterMsrIa32MiscEnable       RegisterName = 0x000020A0
	RegisterIa32FeatureControl      RegisterName = 0x000020A1
	RegisterIa32VmxBasic            RegisterName = 0x000020A2
	RegisterIa32VmxPinbasedCtls     RegisterName = 0x000020A3
	RegisterIa32VmxProcbasedCtls    RegisterName = 0x000020A4
	RegisterIa32VmxExitCtls         RegisterName = 0x000020A5
	RegisterIa32VmxEntryCtls        RegisterName = 0x000020A6
	RegisterIa32VmxMisc             RegisterName = 0x000020A7
	RegisterIa32VmxCr0Fixed0        RegisterName = 0x000020A8
	RegisterIa32VmxCr0Fixed1        RegisterName = 0x000020A9
	RegisterIa32VmxCr4Fixed0        RegisterName = 0x000020AA
	RegisterIa32VmxCr4Fixed1        RegisterName = 0x000020AB
	RegisterIa32VmxVmcsEnum         RegisterName = 0x000020AC
	RegisterIa32VmxProcbasedCtls2   RegisterName = 0x000020AD
	RegisterIa32VmxEptVpidCap       RegisterName = 0x000020AE
	RegisterIa32VmxTruePinbasedCtls RegisterName = 0x000020AF
	RegisterIa32VmxTrueProcbased    RegisterName = 0x000020B0
	RegisterIa32VmxTrueExitCtls     RegisterName = 0x000020B1
	RegisterIa32VmxTrueEntryCtls    RegisterName = 0x000020B2
	RegisterAmdVmHsavePa            RegisterName = 0x000020B3
	RegisterAmdVmCr                 RegisterName = 0x000020B4
)

// X64 APIC State Registers
const (
	RegisterApicId           RegisterName = 0x00003002
	RegisterApicVersion      RegisterName = 0x00003003
	RegisterApicTpr          RegisterName = 0x00003008
	RegisterApicPpr          RegisterName = 0x0000300A
	RegisterApicEoi          RegisterName = 0x0000300B
	RegisterApicLdr          RegisterName = 0x0000300D
	RegisterApicSpurious     RegisterName = 0x0000300F
	RegisterApicIsr0         RegisterName = 0x00003010
	RegisterApicIsr1         RegisterName = 0x00003011
	RegisterApicIsr2         RegisterName = 0x00003012
	RegisterApicIsr3         RegisterName = 0x00003013
	RegisterApicIsr4         RegisterName = 0x00003014
	RegisterApicIsr5         RegisterName = 0x00003015
	RegisterApicIsr6         RegisterName = 0x00003016
	RegisterApicIsr7         RegisterName = 0x00003017
	RegisterApicTmr0         RegisterName = 0x00003018
	RegisterApicTmr1         RegisterName = 0x00003019
	RegisterApicTmr2         RegisterName = 0x0000301A
	RegisterApicTmr3         RegisterName = 0x0000301B
	RegisterApicTmr4         RegisterName = 0x0000301C
	RegisterApicTmr5         RegisterName = 0x0000301D
	RegisterApicTmr6         RegisterName = 0x0000301E
	RegisterApicTmr7         RegisterName = 0x0000301F
	RegisterApicIrr0         RegisterName = 0x00003020
	RegisterApicIrr1         RegisterName = 0x00003021
	RegisterApicIrr2         RegisterName = 0x00003022
	RegisterApicIrr3         RegisterName = 0x00003023
	RegisterApicIrr4         RegisterName = 0x00003024
	RegisterApicIrr5         RegisterName = 0x00003025
	RegisterApicIrr6         RegisterName = 0x00003026
	RegisterApicIrr7         RegisterName = 0x00003027
	RegisterApicEse          RegisterName = 0x00003028
	RegisterApicIcr          RegisterName = 0x00003030
	RegisterApicLvtTimer     RegisterName = 0x00003032
	RegisterApicLvtThermal   RegisterName = 0x00003033
	RegisterApicLvtPerfmon   RegisterName = 0x00003034
	RegisterApicLvtLint0     RegisterName = 0x00003035
	RegisterApicLvtLint1     RegisterName = 0x00003036
	RegisterApicLvtError     RegisterName = 0x00003037
	RegisterApicInitCount    RegisterName = 0x00003038
	RegisterApicCurrentCount RegisterName = 0x00003039
	RegisterApicDivide       RegisterName = 0x0000303E
	RegisterApicSelfIpi      RegisterName = 0x0000303F
)

// Synic Registers
const (
	RegisterSint0    RegisterName = 0x00004000
	RegisterSint1    RegisterName = 0x00004001
	RegisterSint2    RegisterName = 0x00004002
	RegisterSint3    RegisterName = 0x00004003
	RegisterSint4    RegisterName = 0x00004004
	RegisterSint5    RegisterName = 0x00004005
	RegisterSint6    RegisterName = 0x00004006
	RegisterSint7    RegisterName = 0x00004007
	RegisterSint8    RegisterName = 0x00004008
	RegisterSint9    RegisterName = 0x00004009
	RegisterSint10   RegisterName = 0x0000400A
	RegisterSint11   RegisterName = 0x0000400B
	RegisterSint12   RegisterName = 0x0000400C
	RegisterSint13   RegisterName = 0x0000400D
	RegisterSint14   RegisterName = 0x0000400E
	RegisterSint15   RegisterName = 0x0000400F
	RegisterScontrol RegisterName = 0x00004010
	RegisterSversion RegisterName = 0x00004011
	RegisterSiefp    RegisterName = 0x00004012
	RegisterSimp     RegisterName = 0x00004013
	RegisterEom      RegisterName = 0x00004014
)

// Hypervisor Defined Registers
const (
	RegisterVpRuntime            RegisterName = 0x00005000
	RegisterHypercall            RegisterName = 0x00005001
	RegisterGuestOsId            RegisterName = 0x00005002
	RegisterVpAssistPage         RegisterName = 0x00005013
	RegisterReferenceTsc         RegisterName = 0x00005017
	RegisterReferenceTscSequence RegisterName = 0x0000501A
	RegisterNestedGuestState     RegisterName = 0x00005050
	RegisterNestedCurrentVmGpa   RegisterName = 0x00005051
	RegisterNestedVmxInvEpt      RegisterName = 0x00005052
	RegisterNestedVmxInvVpid     RegisterName = 0x00005053
)

// Interrupt / Event Registers
const (
	RegisterPendingInterruption         RegisterName = 0x80000000
	RegisterInterruptState              RegisterName = 0x80000001
	RegisterPendingEvent                RegisterName = 0x80000002
	RegisterPendingEvent1               RegisterName = 0x80000003
	RegisterDeliverabilityNotifications RegisterName = 0x80000004
	RegisterInternalActivityState       RegisterName = 0x80000005
	RegisterPendingDebugException       RegisterName = 0x80000006
	RegisterPendingEvent2               RegisterName = 0x80000007
	RegisterPendingEvent3               RegisterName = 0x80000008
)

// ARM64 General Purpose Registers
const (
	Arm64RegisterX0  RegisterName = 0x00020000
	Arm64RegisterX1  RegisterName = 0x00020001
	Arm64RegisterX2  RegisterName = 0x00020002
	Arm64RegisterX3  RegisterName = 0x00020003
	Arm64RegisterX4  RegisterName = 0x00020004
	Arm64RegisterX5  RegisterName = 0x00020005
	Arm64RegisterX6  RegisterName = 0x00020006
	Arm64RegisterX7  RegisterName = 0x00020007
	Arm64RegisterX8  RegisterName = 0x00020008
	Arm64RegisterX9  RegisterName = 0x00020009
	Arm64RegisterX10 RegisterName = 0x0002000A
	Arm64RegisterX11 RegisterName = 0x0002000B
	Arm64RegisterX12 RegisterName = 0x0002000C
	Arm64RegisterX13 RegisterName = 0x0002000D
	Arm64RegisterX14 RegisterName = 0x0002000E
	Arm64RegisterX15 RegisterName = 0x0002000F
	Arm64RegisterX16 RegisterName = 0x00020010
	Arm64RegisterX17 RegisterName = 0x00020011
	Arm64RegisterX18 RegisterName = 0x00020012
	Arm64RegisterX19 RegisterName = 0x00020013
	Arm64RegisterX20 RegisterName = 0x00020014
	Arm64RegisterX21 RegisterName = 0x00020015
	Arm64RegisterX22 RegisterName = 0x00020016
	Arm64RegisterX23 RegisterName = 0x00020017
	Arm64RegisterX24 RegisterName = 0x00020018
	Arm64RegisterX25 RegisterName = 0x00020019
	Arm64RegisterX26 RegisterName = 0x0002001A
	Arm64RegisterX27 RegisterName = 0x0002001B
	Arm64RegisterX28 RegisterName = 0x0002001C
	Arm64RegisterFp  RegisterName = 0x0002001D
	Arm64RegisterLr  RegisterName = 0x0002001E
	Arm64RegisterPc  RegisterName = 0x00020022
)

// ARM64 Floating Point Registers
const (
	Arm64RegisterQ0  RegisterName = 0x00030000
	Arm64RegisterQ1  RegisterName = 0x00030001
	Arm64RegisterQ2  RegisterName = 0x00030002
	Arm64RegisterQ3  RegisterName = 0x00030003
	Arm64RegisterQ4  RegisterName = 0x00030004
	Arm64RegisterQ5  RegisterName = 0x00030005
	Arm64RegisterQ6  RegisterName = 0x00030006
	Arm64RegisterQ7  RegisterName = 0x00030007
	Arm64RegisterQ8  RegisterName = 0x00030008
	Arm64RegisterQ9  RegisterName = 0x00030009
	Arm64RegisterQ10 RegisterName = 0x0003000A
	Arm64RegisterQ11 RegisterName = 0x0003000B
	Arm64RegisterQ12 RegisterName = 0x0003000C
	Arm64RegisterQ13 RegisterName = 0x0003000D
	Arm64RegisterQ14 RegisterName = 0x0003000E
	Arm64RegisterQ15 RegisterName = 0x0003000F
	Arm64RegisterQ16 RegisterName = 0x00030010
	Arm64RegisterQ17 RegisterName = 0x00030011
	Arm64RegisterQ18 RegisterName = 0x00030012
	Arm64RegisterQ19 RegisterName = 0x00030013
	Arm64RegisterQ20 RegisterName = 0x00030014
	Arm64RegisterQ21 RegisterName = 0x00030015
	Arm64RegisterQ22 RegisterName = 0x00030016
	Arm64RegisterQ23 RegisterName = 0x00030017
	Arm64RegisterQ24 RegisterName = 0x00030018
	Arm64RegisterQ25 RegisterName = 0x00030019
	Arm64RegisterQ26 RegisterName = 0x0003001A
	Arm64RegisterQ27 RegisterName = 0x0003001B
	Arm64RegisterQ28 RegisterName = 0x0003001C
	Arm64RegisterQ29 RegisterName = 0x0003001D
	Arm64RegisterQ30 RegisterName = 0x0003001E
	Arm64RegisterQ31 RegisterName = 0x0003001F
)

// ARM64 Special Purpose Registers
const (
	Arm64RegisterPstate  RegisterName = 0x00020023
	Arm64RegisterElrEl1  RegisterName = 0x00040015
	Arm64RegisterFpcr    RegisterName = 0x00040012
	Arm64RegisterFpsr    RegisterName = 0x00040013
	Arm64RegisterSp      RegisterName = 0x0002001F
	Arm64RegisterSpEl0   RegisterName = 0x00020020
	Arm64RegisterSpEl1   RegisterName = 0x00020021
	Arm64RegisterSpsrEl1 RegisterName = 0x00040014
)

// ARM64 ID Registers
const (
	Arm64RegisterIdMidrEl1      RegisterName = 0x00022000
	Arm64RegisterIdRes01El1     RegisterName = 0x00022001
	Arm64RegisterIdRes02El1     RegisterName = 0x00022002
	Arm64RegisterIdRes03El1     RegisterName = 0x00022003
	Arm64RegisterIdRes04El1     RegisterName = 0x00022004
	Arm64RegisterIdMpidrEl1     RegisterName = 0x00022005
	Arm64RegisterIdRevidrEl1    RegisterName = 0x00022006
	Arm64RegisterIdRes07El1     RegisterName = 0x00022007
	Arm64RegisterIdPfr0El1      RegisterName = 0x00022008
	Arm64RegisterIdPfr1El1      RegisterName = 0x00022009
	Arm64RegisterIdDfr0El1      RegisterName = 0x0002200A
	Arm64RegisterIdRes13El1     RegisterName = 0x0002200B
	Arm64RegisterIdMmfr0El1     RegisterName = 0x0002200C
	Arm64RegisterIdMmfr1El1     RegisterName = 0x0002200D
	Arm64RegisterIdMmfr2El1     RegisterName = 0x0002200E
	Arm64RegisterIdMmfr3El1     RegisterName = 0x0002200F
	Arm64RegisterIdIsar0El1     RegisterName = 0x00022010
	Arm64RegisterIdIsar1El1     RegisterName = 0x00022011
	Arm64RegisterIdIsar2El1     RegisterName = 0x00022012
	Arm64RegisterIdIsar3El1     RegisterName = 0x00022013
	Arm64RegisterIdIsar4El1     RegisterName = 0x00022014
	Arm64RegisterIdIsar5El1     RegisterName = 0x00022015
	Arm64RegisterIdRes26El1     RegisterName = 0x00022016
	Arm64RegisterIdRes27El1     RegisterName = 0x00022017
	Arm64RegisterIdMvfr0El1     RegisterName = 0x00022018
	Arm64RegisterIdMvfr1El1     RegisterName = 0x00022019
	Arm64RegisterIdMvfr2El1     RegisterName = 0x0002201A
	Arm64RegisterIdRes33El1     RegisterName = 0x0002201B
	Arm64RegisterIdPfr2El1      RegisterName = 0x0002201C
	Arm64RegisterIdRes35El1     RegisterName = 0x0002201D
	Arm64RegisterIdRes36El1     RegisterName = 0x0002201E
	Arm64RegisterIdRes37El1     RegisterName = 0x0002201F
	Arm64RegisterIdAa64Pfr0El1  RegisterName = 0x00022020
	Arm64RegisterIdAa64Pfr1El1  RegisterName = 0x00022021
	Arm64RegisterIdAa64Pfr2El1  RegisterName = 0x00022022
	Arm64RegisterIdRes43El1     RegisterName = 0x00022023
	Arm64RegisterIdAa64Zfr0El1  RegisterName = 0x00022024
	Arm64RegisterIdAa64Smfr0El1 RegisterName = 0x00022025
	Arm64RegisterIdRes46El1     RegisterName = 0x00022026
	Arm64RegisterIdRes47El1     RegisterName = 0x00022027
	Arm64RegisterIdAa64Dfr0El1  RegisterName = 0x00022028
	Arm64RegisterIdAa64Dfr1El1  RegisterName = 0x00022029
	Arm64RegisterIdRes52El1     RegisterName = 0x0002202A
	Arm64RegisterIdRes53El1     RegisterName = 0x0002202B
	Arm64RegisterIdRes54El1     RegisterName = 0x0002202C
	Arm64RegisterIdRes55El1     RegisterName = 0x0002202D
	Arm64RegisterIdRes56El1     RegisterName = 0x0002202E
	Arm64RegisterIdRes57El1     RegisterName = 0x0002202F
	Arm64RegisterIdAa64Isar0El1 RegisterName = 0x00022030
	Arm64RegisterIdAa64Isar1El1 RegisterName = 0x00022031
	Arm64RegisterIdAa64Isar2El1 RegisterName = 0x00022032
	Arm64RegisterIdRes63El1     RegisterName = 0x00022033
	Arm64RegisterIdRes64El1     RegisterName = 0x00022034
	Arm64RegisterIdRes65El1     RegisterName = 0x00022035
	Arm64RegisterIdRes66El1     RegisterName = 0x00022036
	Arm64RegisterIdRes67El1     RegisterName = 0x00022037
	Arm64RegisterIdAa64Mmfr0El1 RegisterName = 0x00022038
	Arm64RegisterIdAa64Mmfr1El1 RegisterName = 0x00022039
	Arm64RegisterIdAa64Mmfr2El1 RegisterName = 0x0002203A
	Arm64RegisterIdAa64Mmfr3El1 RegisterName = 0x0002203B
	Arm64RegisterIdAa64Mmfr4El1 RegisterName = 0x0002203C
	Arm64RegisterIdRes75El1     RegisterName = 0x0002203D
	Arm64RegisterIdRes76El1     RegisterName = 0x0002203E
	Arm64RegisterIdRes77El1     RegisterName = 0x0002203F
	Arm64RegisterIdRes80El1     RegisterName = 0x00022040
	Arm64RegisterIdRes81El1     RegisterName = 0x00022041
	Arm64RegisterIdRes82El1     RegisterName = 0x00022042
	Arm64RegisterIdRes83El1     RegisterName = 0x00022043
	Arm64RegisterIdRes84El1     RegisterName = 0x00022044
	Arm64RegisterIdRes85El1     RegisterName = 0x00022045
	Arm64RegisterIdRes86El1     RegisterName = 0x00022046
	Arm64RegisterIdRes87El1     RegisterName = 0x00022047
	Arm64RegisterIdRes90El1     RegisterName = 0x00022048
	Arm64RegisterIdRes91El1     RegisterName = 0x00022049
	Arm64RegisterIdRes92El1     RegisterName = 0x0002204A
	Arm64RegisterIdRes93El1     RegisterName = 0x0002204B
	Arm64RegisterIdRes94El1     RegisterName = 0x0002204C
	Arm64RegisterIdRes95El1     RegisterName = 0x0002204D
	Arm64RegisterIdRes96El1     RegisterName = 0x0002204E
	Arm64RegisterIdRes97El1     RegisterName = 0x0002204F
	Arm64RegisterIdRes100El1    RegisterName = 0x00022050
	Arm64RegisterIdRes101El1    RegisterName = 0x00022051
	Arm64RegisterIdRes102El1    RegisterName = 0x00022052
	Arm64RegisterIdRes103El1    RegisterName = 0x00022053
	Arm64RegisterIdRes104El1    RegisterName = 0x00022054
	Arm64RegisterIdRes105El1    RegisterName = 0x00022055
	Arm64RegisterIdRes106El1    RegisterName = 0x00022056
	Arm64RegisterIdRes107El1    RegisterName = 0x00022057
	Arm64RegisterIdRes110El1    RegisterName = 0x00022058
	Arm64RegisterIdRes111El1    RegisterName = 0x00022059
	Arm64RegisterIdRes112El1    RegisterName = 0x0002205A
	Arm64RegisterIdRes113El1    RegisterName = 0x0002205B
	Arm64RegisterIdRes114El1    RegisterName = 0x0002205C
	Arm64RegisterIdRes115El1    RegisterName = 0x0002205D
	Arm64RegisterIdRes116El1    RegisterName = 0x0002205E
	Arm64RegisterIdRes117El1    RegisterName = 0x0002205F
	Arm64RegisterIdRes120El1    RegisterName = 0x00022060
	Arm64RegisterIdRes121El1    RegisterName = 0x00022061
	Arm64RegisterIdRes122El1    RegisterName = 0x00022062
	Arm64RegisterIdRes123El1    RegisterName = 0x00022063
	Arm64RegisterIdRes124El1    RegisterName = 0x00022064
	Arm64RegisterIdRes125El1    RegisterName = 0x00022065
	Arm64RegisterIdRes126El1    RegisterName = 0x00022066
	Arm64RegisterIdRes127El1    RegisterName = 0x00022067
	Arm64RegisterIdRes130El1    RegisterName = 0x00022068
	Arm64RegisterIdRes131El1    RegisterName = 0x00022069
	Arm64RegisterIdRes132El1    RegisterName = 0x0002206A
	Arm64RegisterIdRes133El1    RegisterName = 0x0002206B
	Arm64RegisterIdRes134El1    RegisterName = 0x0002206C
	Arm64RegisterIdRes135El1    RegisterName = 0x0002206D
	Arm64RegisterIdRes136El1    RegisterName = 0x0002206E
	Arm64RegisterIdRes137El1    RegisterName = 0x0002206F
	Arm64RegisterIdRes140El1    RegisterName = 0x00022070
	Arm64RegisterIdRes141El1    RegisterName = 0x00022071
	Arm64RegisterIdRes142El1    RegisterName = 0x00022072
	Arm64RegisterIdRes143El1    RegisterName = 0x00022073
	Arm64RegisterIdRes144El1    RegisterName = 0x00022074
	Arm64RegisterIdRes145El1    RegisterName = 0x00022075
	Arm64RegisterIdRes146El1    RegisterName = 0x00022076
	Arm64RegisterIdRes147El1    RegisterName = 0x00022077
	Arm64RegisterIdRes150El1    RegisterName = 0x00022078
	Arm64RegisterIdRes151El1    RegisterName = 0x00022079
	Arm64RegisterIdRes152El1    RegisterName = 0x0002207A
	Arm64RegisterIdRes153El1    RegisterName = 0x0002207B
	Arm64RegisterIdRes154El1    RegisterName = 0x0002207C
	Arm64RegisterIdRes155El1    RegisterName = 0x0002207D
	Arm64RegisterIdRes156El1    RegisterName = 0x0002207E
	Arm64RegisterIdRes157El1    RegisterName = 0x0002207F
)

// ARM64 General System Control Registers
const (
	Arm64RegisterActlrEl1      RegisterName = 0x00040003
	Arm64RegisterApdAKeyHiEl1  RegisterName = 0x00040026
	Arm64RegisterApdAKeyLoEl1  RegisterName = 0x00040027
	Arm64RegisterApdBKeyHiEl1  RegisterName = 0x00040028
	Arm64RegisterApdBKeyLoEl1  RegisterName = 0x00040029
	Arm64RegisterApgAKeyHiEl1  RegisterName = 0x0004002A
	Arm64RegisterApgAKeyLoEl1  RegisterName = 0x0004002B
	Arm64RegisterApiAKeyHiEl1  RegisterName = 0x0004002C
	Arm64RegisterApiAKeyLoEl1  RegisterName = 0x0004002D
	Arm64RegisterApiBKeyHiEl1  RegisterName = 0x0004002E
	Arm64RegisterApiBKeyLoEl1  RegisterName = 0x0004002F
	Arm64RegisterContextidrEl1 RegisterName = 0x0004000D
	Arm64RegisterCpacrEl1      RegisterName = 0x00040004
	Arm64RegisterCsselrEl1     RegisterName = 0x00040035
	Arm64RegisterEsrEl1        RegisterName = 0x00040008
	Arm64RegisterFarEl1        RegisterName = 0x00040009
	Arm64RegisterMairEl1       RegisterName = 0x0004000B
	Arm64RegisterMidrEl1       RegisterName = 0x00040051
	Arm64RegisterMpidrEl1      RegisterName = 0x00040001
	Arm64RegisterParEl1        RegisterName = 0x0004000A
	Arm64RegisterSctlrEl1      RegisterName = 0x00040002
	Arm64RegisterTcrEl1        RegisterName = 0x00040007
	Arm64RegisterTpidrEl0      RegisterName = 0x00040011
	Arm64RegisterTpidrEl1      RegisterName = 0x0004000E
	Arm64RegisterTpidrroEl0    RegisterName = 0x00040010
	Arm64RegisterTtbr0El1      RegisterName = 0x00040005
	Arm64RegisterTtbr1El1      RegisterName = 0x00040006
	Arm64RegisterVbarEl1       RegisterName = 0x0004000C
	Arm64RegisterZcrEl1        RegisterName = 0x00040071
)

// ARM64 Debug Registers
const (
	Arm64RegisterDbgbcr0El1  RegisterName = 0x00050000
	Arm64RegisterDbgbcr1El1  RegisterName = 0x00050001
	Arm64RegisterDbgbcr2El1  RegisterName = 0x00050002
	Arm64RegisterDbgbcr3El1  RegisterName = 0x00050003
	Arm64RegisterDbgbcr4El1  RegisterName = 0x00050004
	Arm64RegisterDbgbcr5El1  RegisterName = 0x00050005
	Arm64RegisterDbgbcr6El1  RegisterName = 0x00050006
	Arm64RegisterDbgbcr7El1  RegisterName = 0x00050007
	Arm64RegisterDbgbcr8El1  RegisterName = 0x00050008
	Arm64RegisterDbgbcr9El1  RegisterName = 0x00050009
	Arm64RegisterDbgbcr10El1 RegisterName = 0x0005000A
	Arm64RegisterDbgbcr11El1 RegisterName = 0x0005000B
	Arm64RegisterDbgbcr12El1 RegisterName = 0x0005000C
	Arm64RegisterDbgbcr13El1 RegisterName = 0x0005000D
	Arm64RegisterDbgbcr14El1 RegisterName = 0x0005000E
	Arm64RegisterDbgbcr15El1 RegisterName = 0x0005000F
	Arm64RegisterDbgbvr0El1  RegisterName = 0x00050020
	Arm64RegisterDbgbvr1El1  RegisterName = 0x00050021
	Arm64RegisterDbgbvr2El1  RegisterName = 0x00050022
	Arm64RegisterDbgbvr3El1  RegisterName = 0x00050023
	Arm64RegisterDbgbvr4El1  RegisterName = 0x00050024
	Arm64RegisterDbgbvr5El1  RegisterName = 0x00050025
	Arm64RegisterDbgbvr6El1  RegisterName = 0x00050026
	Arm64RegisterDbgbvr7El1  RegisterName = 0x00050027
	Arm64RegisterDbgbvr8El1  RegisterName = 0x00050028
	Arm64RegisterDbgbvr9El1  RegisterName = 0x00050029
	Arm64RegisterDbgbvr10El1 RegisterName = 0x0005002A
	Arm64RegisterDbgbvr11El1 RegisterName = 0x0005002B
	Arm64RegisterDbgbvr12El1 RegisterName = 0x0005002C
	Arm64RegisterDbgbvr13El1 RegisterName = 0x0005002D
	Arm64RegisterDbgbvr14El1 RegisterName = 0x0005002E
	Arm64RegisterDbgbvr15El1 RegisterName = 0x0005002F
	Arm64RegisterDbgprcrEl1  RegisterName = 0x00050045
	Arm64RegisterDbgwcr0El1  RegisterName = 0x00050010
	Arm64RegisterDbgwcr1El1  RegisterName = 0x00050011
	Arm64RegisterDbgwcr2El1  RegisterName = 0x00050012
	Arm64RegisterDbgwcr3El1  RegisterName = 0x00050013
	Arm64RegisterDbgwcr4El1  RegisterName = 0x00050014
	Arm64RegisterDbgwcr5El1  RegisterName = 0x00050015
	Arm64RegisterDbgwcr6El1  RegisterName = 0x00050016
	Arm64RegisterDbgwcr7El1  RegisterName = 0x00050017
	Arm64RegisterDbgwcr8El1  RegisterName = 0x00050018
	Arm64RegisterDbgwcr9El1  RegisterName = 0x00050019
	Arm64RegisterDbgwcr10El1 RegisterName = 0x0005001A
	Arm64RegisterDbgwcr11El1 RegisterName = 0x0005001B
	Arm64RegisterDbgwcr12El1 RegisterName = 0x0005001C
	Arm64RegisterDbgwcr13El1 RegisterName = 0x0005001D
	Arm64RegisterDbgwcr14El1 RegisterName = 0x0005001E
	Arm64RegisterDbgwcr15El1 RegisterName = 0x0005001F
	Arm64RegisterDbgwvr0El1  RegisterName = 0x00050030
	Arm64RegisterDbgwvr1El1  RegisterName = 0x00050031
	Arm64RegisterDbgwvr2El1  RegisterName = 0x00050032
	Arm64RegisterDbgwvr3El1  RegisterName = 0x00050033
	Arm64RegisterDbgwvr4El1  RegisterName = 0x00050034
	Arm64RegisterDbgwvr5El1  RegisterName = 0x00050035
	Arm64RegisterDbgwvr6El1  RegisterName = 0x00050036
	Arm64RegisterDbgwvr7El1  RegisterName = 0x00050037
	Arm64RegisterDbgwvr8El1  RegisterName = 0x00050038
	Arm64RegisterDbgwvr9El1  RegisterName = 0x00050039
	Arm64RegisterDbgwvr10El1 RegisterName = 0x0005003A
	Arm64RegisterDbgwvr11El1 RegisterName = 0x0005003B
	Arm64RegisterDbgwvr12El1 RegisterName = 0x0005003C
	Arm64RegisterDbgwvr13El1 RegisterName = 0x0005003D
	Arm64RegisterDbgwvr14El1 RegisterName = 0x0005003E
	Arm64RegisterDbgwvr15El1 RegisterName = 0x0005003F
	Arm64RegisterMdrarEl1    RegisterName = 0x0005004C
	Arm64RegisterMdscrEl1    RegisterName = 0x0005004D
	Arm64RegisterOsdlrEl1    RegisterName = 0x0005004E
	Arm64RegisterOslarEl1    RegisterName = 0x00050052
	Arm64RegisterOslsrEl1    RegisterName = 0x00050053
)

// ARM64 Performance Monitors Registers
const (
	Arm64RegisterPmccfiltrEl0   RegisterName = 0x00052000
	Arm64RegisterPmccntrEl0     RegisterName = 0x00052001
	Arm64RegisterPmceid0El0     RegisterName = 0x00052002
	Arm64RegisterPmceid1El0     RegisterName = 0x00052003
	Arm64RegisterPmcntenclrEl0  RegisterName = 0x00052004
	Arm64RegisterPmcntensetEl0  RegisterName = 0x00052005
	Arm64RegisterPmcrEl0        RegisterName = 0x00052006
	Arm64RegisterPmevcntr0El0   RegisterName = 0x00052007
	Arm64RegisterPmevcntr1El0   RegisterName = 0x00052008
	Arm64RegisterPmevcntr2El0   RegisterName = 0x00052009
	Arm64RegisterPmevcntr3El0   RegisterName = 0x0005200A
	Arm64RegisterPmevcntr4El0   RegisterName = 0x0005200B
	Arm64RegisterPmevcntr5El0   RegisterName = 0x0005200C
	Arm64RegisterPmevcntr6El0   RegisterName = 0x0005200D
	Arm64RegisterPmevcntr7El0   RegisterName = 0x0005200E
	Arm64RegisterPmevcntr8El0   RegisterName = 0x0005200F
	Arm64RegisterPmevcntr9El0   RegisterName = 0x00052010
	Arm64RegisterPmevcntr10El0  RegisterName = 0x00052011
	Arm64RegisterPmevcntr11El0  RegisterName = 0x00052012
	Arm64RegisterPmevcntr12El0  RegisterName = 0x00052013
	Arm64RegisterPmevcntr13El0  RegisterName = 0x00052014
	Arm64RegisterPmevcntr14El0  RegisterName = 0x00052015
	Arm64RegisterPmevcntr15El0  RegisterName = 0x00052016
	Arm64RegisterPmevcntr16El0  RegisterName = 0x00052017
	Arm64RegisterPmevcntr17El0  RegisterName = 0x00052018
	Arm64RegisterPmevcntr18El0  RegisterName = 0x00052019
	Arm64RegisterPmevcntr19El0  RegisterName = 0x0005201A
	Arm64RegisterPmevcntr20El0  RegisterName = 0x0005201B
	Arm64RegisterPmevcntr21El0  RegisterName = 0x0005201C
	Arm64RegisterPmevcntr22El0  RegisterName = 0x0005201D
	Arm64RegisterPmevcntr23El0  RegisterName = 0x0005201E
	Arm64RegisterPmevcntr24El0  RegisterName = 0x0005201F
	Arm64RegisterPmevcntr25El0  RegisterName = 0x00052020
	Arm64RegisterPmevcntr26El0  RegisterName = 0x00052021
	Arm64RegisterPmevcntr27El0  RegisterName = 0x00052022
	Arm64RegisterPmevcntr28El0  RegisterName = 0x00052023
	Arm64RegisterPmevcntr29El0  RegisterName = 0x00052024
	Arm64RegisterPmevcntr30El0  RegisterName = 0x00052025
	Arm64RegisterPmevtyper0El0  RegisterName = 0x00052026
	Arm64RegisterPmevtyper1El0  RegisterName = 0x00052027
	Arm64RegisterPmevtyper2El0  RegisterName = 0x00052028
	Arm64RegisterPmevtyper3El0  RegisterName = 0x00052029
	Arm64RegisterPmevtyper4El0  RegisterName = 0x0005202A
	Arm64RegisterPmevtyper5El0  RegisterName = 0x0005202B
	Arm64RegisterPmevtyper6El0  RegisterName = 0x0005202C
	Arm64RegisterPmevtyper7El0  RegisterName = 0x0005202D
	Arm64RegisterPmevtyper8El0  RegisterName = 0x0005202E
	Arm64RegisterPmevtyper9El0  RegisterName = 0x0005202F
	Arm64RegisterPmevtyper10El0 RegisterName = 0x00052030
	Arm64RegisterPmevtyper11El0 RegisterName = 0x00052031
	Arm64RegisterPmevtyper12El0 RegisterName = 0x00052032
	Arm64RegisterPmevtyper13El0 RegisterName = 0x00052033
	Arm64RegisterPmevtyper14El0 RegisterName = 0x00052034
	Arm64RegisterPmevtyper15El0 RegisterName = 0x00052035
	Arm64RegisterPmevtyper16El0 RegisterName = 0x00052036
	Arm64RegisterPmevtyper17El0 RegisterName = 0x00052037
	Arm64RegisterPmevtyper18El0 RegisterName = 0x00052038
	Arm64RegisterPmevtyper19El0 RegisterName = 0x00052039
	Arm64RegisterPmevtyper20El0 RegisterName = 0x0005203A
	Arm64RegisterPmevtyper21El0 RegisterName = 0x0005203B
	Arm64RegisterPmevtyper22El0 RegisterName = 0x0005203C
	Arm64RegisterPmevtyper23El0 RegisterName = 0x0005203D
	Arm64RegisterPmevtyper24El0 RegisterName = 0x0005203E
	Arm64RegisterPmevtyper25El0 RegisterName = 0x0005203F
	Arm64RegisterPmevtyper26El0 RegisterName = 0x00052040
	Arm64RegisterPmevtyper27El0 RegisterName = 0x00052041
	Arm64RegisterPmevtyper28El0 RegisterName = 0x00052042
	Arm64RegisterPmevtyper29El0 RegisterName = 0x00052043
	Arm64RegisterPmevtyper30El0 RegisterName = 0x00052044
	Arm64RegisterPmintenclrEl1  RegisterName = 0x00052045
	Arm64RegisterPmintensetEl1  RegisterName = 0x00052046
	Arm64RegisterPmovsclrEl0    RegisterName = 0x00052048
	Arm64RegisterPmovssetEl0    RegisterName = 0x00052049
	Arm64RegisterPmselrEl0      RegisterName = 0x0005204A
	Arm64RegisterPmuserenrEl0   RegisterName = 0x0005204C
)

// ARM64 Generic Timer Registers
const (
	Arm64RegisterCntkctlEl1  RegisterName = 0x00058008
	Arm64RegisterCntvCtlEl0  RegisterName = 0x0005800E
	Arm64RegisterCntvCvalEl0 RegisterName = 0x0005800F
	Arm64RegisterCntvctEl0   RegisterName = 0x00058011
)

// ARM64 GIC Redistributor
const (
	Arm64RegisterGicrBaseGpa RegisterName = 0x00063000
)

// ARM64 Synic Registers
// Note: These share the same WHV_REGISTER_NAME enum names as x64 in C headers
// but have different values. Prefixed here to avoid collisions.
const (
	Arm64RegisterSint0    RegisterName = 0x000A0000
	Arm64RegisterSint1    RegisterName = 0x000A0001
	Arm64RegisterSint2    RegisterName = 0x000A0002
	Arm64RegisterSint3    RegisterName = 0x000A0003
	Arm64RegisterSint4    RegisterName = 0x000A0004
	Arm64RegisterSint5    RegisterName = 0x000A0005
	Arm64RegisterSint6    RegisterName = 0x000A0006
	Arm64RegisterSint7    RegisterName = 0x000A0007
	Arm64RegisterSint8    RegisterName = 0x000A0008
	Arm64RegisterSint9    RegisterName = 0x000A0009
	Arm64RegisterSint10   RegisterName = 0x000A000A
	Arm64RegisterSint11   RegisterName = 0x000A000B
	Arm64RegisterSint12   RegisterName = 0x000A000C
	Arm64RegisterSint13   RegisterName = 0x000A000D
	Arm64RegisterSint14   RegisterName = 0x000A000E
	Arm64RegisterSint15   RegisterName = 0x000A000F
	Arm64RegisterScontrol RegisterName = 0x000A0010
	Arm64RegisterSversion RegisterName = 0x000A0011
	Arm64RegisterSifp     RegisterName = 0x000A0012
	Arm64RegisterSipp     RegisterName = 0x000A0013
	Arm64RegisterEom      RegisterName = 0x000A0014
)

// ARM64 Hypervisor Defined Registers
const (
	Arm64RegisterHypervisorVersion         RegisterName = 0x00000100
	Arm64RegisterPrivilegesAndFeaturesInfo RegisterName = 0x00000200
	Arm64RegisterFeaturesInfo              RegisterName = 0x00000201
	Arm64RegisterImplementationLimitsInfo  RegisterName = 0x00000202
	Arm64RegisterHardwareFeaturesInfo      RegisterName = 0x00000203
	Arm64RegisterCpuManagementFeaturesInfo RegisterName = 0x00000204
	Arm64RegisterPasidFeaturesInfo         RegisterName = 0x00000205
	Arm64RegisterGuestCrashP0              RegisterName = 0x00000210
	Arm64RegisterGuestCrashP1              RegisterName = 0x00000211
	Arm64RegisterGuestCrashP2              RegisterName = 0x00000212
	Arm64RegisterGuestCrashP3              RegisterName = 0x00000213
	Arm64RegisterGuestCrashP4              RegisterName = 0x00000214
	Arm64RegisterGuestCrashCtl             RegisterName = 0x00000215
	Arm64RegisterVpRuntime                 RegisterName = 0x00090000
	Arm64RegisterGuestOsId                 RegisterName = 0x00090002
	Arm64RegisterVpAssistPage              RegisterName = 0x00090013
	Arm64RegisterPartitionInfoPage         RegisterName = 0x00090015
	Arm64RegisterReferenceTsc              RegisterName = 0x00090017
	Arm64RegisterReferenceTscSequence      RegisterName = 0x0009001A
	Arm64RegisterPendingEvent0             RegisterName = 0x00010004
	Arm64RegisterPendingEvent1             RegisterName = 0x00010005
	Arm64RegisterDeliverabilityNotify      RegisterName = 0x00010006
	Arm64RegisterInternalActivityState     RegisterName = 0x00000004
	Arm64RegisterPendingEvent2             RegisterName = 0x00010008
	Arm64RegisterPendingEvent3             RegisterName = 0x00010009
)

func (r RegisterName) String() string {
	switch r {
	case RegisterRax:
		return "RAX"
	case RegisterRcx:
		return "RCX"
	case RegisterRdx:
		return "RDX"
	case RegisterRbx:
		return "RBX"
	case RegisterRsp:
		return "RSP"
	case RegisterRbp:
		return "RBP"
	case RegisterRsi:
		return "RSI"
	case RegisterRdi:
		return "RDI"
	case RegisterR8:
		return "R8"
	case RegisterR9:
		return "R9"
	case RegisterR10:
		return "R10"
	case RegisterR11:
		return "R11"
	case RegisterR12:
		return "R12"
	case RegisterR13:
		return "R13"
	case RegisterR14:
		return "R14"
	case RegisterR15:
		return "R15"
	case RegisterRip:
		return "RIP"
	case RegisterRflags:
		return "RFLAGS"
	case RegisterEs:
		return "ES"
	case RegisterCs:
		return "CS"
	case RegisterSs:
		return "SS"
	case RegisterDs:
		return "DS"
	case RegisterFs:
		return "FS"
	case RegisterGs:
		return "GS"
	case RegisterLdtr:
		return "LDTR"
	case RegisterTr:
		return "TR"
	case RegisterIdtr:
		return "IDTR"
	case RegisterGdtr:
		return "GDTR"
	case RegisterCr0:
		return "CR0"
	case RegisterCr2:
		return "CR2"
	case RegisterCr3:
		return "CR3"
	case RegisterCr4:
		return "CR4"
	case RegisterCr8:
		return "CR8"
	case RegisterDr0:
		return "DR0"
	case RegisterDr1:
		return "DR1"
	case RegisterDr2:
		return "DR2"
	case RegisterDr3:
		return "DR3"
	case RegisterDr6:
		return "DR6"
	case RegisterDr7:
		return "DR7"
	case RegisterXCr0:
		return "XCR0"
	case RegisterEfer:
		return "EFER"
	case RegisterTsc:
		return "TSC"
	case RegisterKernelGsBase:
		return "KernelGsBase"
	case RegisterApicBase:
		return "ApicBase"
	case RegisterPendingInterruption:
		return "PendingInterruption"
	case RegisterInterruptState:
		return "InterruptState"
	case RegisterPendingEvent:
		return "PendingEvent"
	case RegisterDeliverabilityNotifications:
		return "DeliverabilityNotifications"
	case Arm64RegisterPc:
		return "Arm64_PC"
	case Arm64RegisterLr:
		return "Arm64_LR"
	case Arm64RegisterSp:
		return "Arm64_SP"
	case Arm64RegisterFp:
		return "Arm64_FP"
	default:
		return fmt.Sprintf("RegisterName(0x%X)", uint32(r))
	}
}
