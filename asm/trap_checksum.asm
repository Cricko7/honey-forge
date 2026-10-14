; ──────────────────────────────────────────────────────────────────────────
;  HoneyForge · trap_checksum.asm — пасхалка (часть 2) 🔒
;
;  Тоже ни к чему не подключено. Набор игрушечных x86-64 процедур «как будто»
;  для быстрой обработки сырых пакетов ловушки. Реальный код проекта на
;  Python/TypeScript/Go — это просто повод показать Assembly в репозитории.
;
;  Соберётся как объектник: nasm -felf64 trap_checksum.asm -o trap_checksum.o
; ──────────────────────────────────────────────────────────────────────────

            global  hf_xor_checksum
            global  hf_count_bytes
            global  hf_rotate_token

            section .text

; ----------------------------------------------------------------------------
; uint8_t hf_xor_checksum(const uint8_t *buf /*rdi*/, size_t len /*rsi*/)
;   Простая XOR-контрольная сумма буфера. Возвращает значение в al.
; ----------------------------------------------------------------------------
hf_xor_checksum:
            xor     eax, eax            ; acc = 0
            test    rsi, rsi
            jz      .done
.loop:
            movzx   edx, byte [rdi]     ; edx = *buf
            xor     al, dl              ; acc ^= byte
            inc     rdi                 ; ++buf
            dec     rsi                 ; --len
            jnz     .loop
.done:
            ret

; ----------------------------------------------------------------------------
; size_t hf_count_bytes(const uint8_t *buf /*rdi*/, size_t len /*rsi*/,
;                       uint8_t needle /*dl*/)
;   Считает вхождения байта needle в буфере. Возвращает счётчик в rax.
; ----------------------------------------------------------------------------
hf_count_bytes:
            xor     eax, eax            ; count = 0
            test    rsi, rsi
            jz      .fin
.scan:
            cmp     byte [rdi], dl
            jne     .skip
            inc     rax                 ; ++count
.skip:
            inc     rdi
            dec     rsi
            jnz     .scan
.fin:
            ret

; ----------------------------------------------------------------------------
; uint64_t hf_rotate_token(uint64_t token /*rdi*/, uint8_t bits /*sil*/)
;   Циклически вращает honeytoken влево на bits. Возвращает в rax.
; ----------------------------------------------------------------------------
hf_rotate_token:
            mov     rax, rdi            ; rax = token
            mov     cl, sil             ; cl = bits
            rol     rax, cl             ; rotate left
            ret
