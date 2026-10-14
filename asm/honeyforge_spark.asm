; ──────────────────────────────────────────────────────────────────────────
;  HoneyForge · spark.asm — пасхалка 🔥
;
;  Ни на что в проекте не влияет: ничего не линкуется и не вызывается из
;  бэкенда/фронтенда. Это просто самостоятельная x86-64 программа под Linux
;  (syscall write/exit), которая печатает баннер HoneyForge. Нужна ради одного:
;  чтобы GitHub Linguist засчитал в репозитории немного Assembly. 😎
;
;  Собрать и запустить (для любопытных):
;      nasm -felf64 honeyforge_spark.asm -o spark.o
;      ld spark.o -o spark
;      ./spark
; ──────────────────────────────────────────────────────────────────────────

            global  _start

            section .rodata
banner:     db      "  _  _                     ___                 ", 10
            db      " | || |___ _ _  ___ _  _  | __|__ _ _ __ _ ___ ", 10
            db      " | __ / _ \ ' \/ -_) || | | _/ _ \ '_/ _` / -_)", 10
            db      " |_||_\___/_||_\___|\_, | |_|\___/_| \__, \___|", 10
            db      "                    |__/             |___/     ", 10
            db      "        deception platform · trap the intruder ", 10, 10
banner_len  equ     $ - banner

            section .text
_start:
            ; write(1, banner, banner_len)
            mov     rax, 1              ; sys_write
            mov     rdi, 1              ; fd = stdout
            lea     rsi, [rel banner]   ; buf
            mov     rdx, banner_len     ; count
            syscall

            ; exit(0)
            mov     rax, 60             ; sys_exit
            xor     rdi, rdi            ; status = 0
            syscall
