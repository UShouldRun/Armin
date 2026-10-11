---
name: riscv-asm
description: Read, write and debug RISC-V assembly (RV32IM and custom extensions) — encodings, the calling convention, custom-instruction opcode space, and checking against the ISA manual. Load when working with RISC-V assembly, encodings or simulators.
---
## Ground truth
Check encodings against the ISA manual (the project may keep one,
e.g. `riscv_isa.pdf`) or the official spec — never from memory
alone. Show the bit layout when encoding by hand.

## Encoding formats (32-bit)
```
R: funct7[31:25] rs2[24:20] rs1[19:15] funct3[14:12] rd[11:7] opcode[6:0]
I: imm[11:0][31:20]          rs1        funct3        rd       opcode
S: imm[11:5]  rs2            rs1        funct3   imm[4:0]      opcode
B: imm[12|10:5] rs2          rs1        funct3   imm[4:1|11]   opcode
U: imm[31:12]                                         rd       opcode
J: imm[20|10:1|11|19:12]                              rd       opcode
```
- Immediates are sign-extended; B and J offsets are in multiples of
  2 (bit 0 implicit). Branch range ±4 KiB, JAL ±1 MiB.
- Base opcodes: OP 0110011, OP-IMM 0010011, LOAD 0000011, STORE
  0100011, BRANCH 1100011, JAL 1101111, JALR 1100111, LUI 0110111,
  AUIPC 0010111. M extension: OP with funct7 0000001.
- Custom instructions go in the reserved custom opcodes
  (custom-0 0001011, custom-1 0101011, custom-2 1011011, custom-3
  1111011), usually R-type, with funct3/funct7 selecting the
  operation. Document every custom encoding in one table.

## Calling convention (ILP32)
- `a0–a7` arguments (`a0–a1` return), `ra` return address, `sp`
  16-byte aligned, `s0–s11` callee-saved, `t0–t6` caller-saved,
  `zero` hardwired.
- Prologue/epilogue save `ra` and used `s` registers.

## Pseudo-instructions
`li` (lui + addi, mind the sign of the low 12 bits: add 1 to the
upper part when bit 11 is set), `la`, `mv`, `j`, `ret`, `call`,
`nop`, `beqz`, `bnez`. Know what each expands to when counting
cycles or offsets.

## Debugging
- Disassemble with `riscv64-unknown-elf-objdump -d` (or the
  project's simulator) and compare against the expected encoding.
- In a simulator, single-step and dump registers around the faulty
  instruction; check alignment on loads/stores.
