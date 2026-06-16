// quick skill: pubblica una Agent Skill (SKILL.md) che insegna a un agente cos'è
// la CLI quick e come usarla. SKILL.md è un formato aperto e cross-vendor (Claude
// Code, Codex CLI, Gemini CLI, Cursor, …): tutti cercano le skill nello stesso
// schema di cartelle, ~/.<tool>/skills/<nome>/ (globale) o .<tool>/skills/<nome>/
// (progetto). Il comando sfrutta questo schema invece di cablare percorsi per tool.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skillDoc è il contenuto della Agent Skill. Frontmatter conforme: name in
// minuscolo, description con "cosa fa" + "quando attivarla".
const skillDoc = `---
name: quick
description: >-
  Pubblica e gestisce siti statici sull'hosting interno "quick" con la CLI ` + "`quick`" + `.
  Usala quando l'utente vuole mettere online una cartella di HTML/asset e ottenere
  un URL <nome>.<dominio>, ripubblicare un sito, cambiarne la visibilità (pubblico,
  codice di accesso, SSO aziendale), bloccarlo o eliminarlo, controllarne lo stato,
  o capire perché un deploy esclude certi file. Copre deploy a specchio (mirror),
  .quickignore, le convenzioni 404.html / 200.html / URL puliti, login Google e stato.
---

# quick CLI

` + "`quick`" + ` è la CLI di un hosting statico interno: pubblichi una cartella di
HTML/asset e ottieni ` + "`https://<nome>.<dominio>`" + `. Di default un sito è visibile
solo agli account del dominio aziendale (SSO Google); puoi aprirlo al pubblico,
proteggerlo con un codice, o bloccarne la sovrascrittura.

Il server si configura da solo: l'unico dato necessario è l'URL, via ` + "`--server`" + `
o la variabile ` + "`QUICK_SERVER`" + `. Dopo il primo deploy, la cartella ricorda sito
e server in un file ` + "`.quick`" + `, quindi i comandi successivi non hanno bisogno di
argomenti.

## Primo uso

` + "```bash" + `
export QUICK_SERVER=https://quick.example.com   # una volta (oppure usa --server)
quick login                                     # apre il browser per il login Google
quick deploy mio-sito ./build                   # -> https://mio-sito.quick.example.com
` + "```" + `

## Comandi

| Comando | Cosa fa |
|---|---|
| ` + "`quick`" + ` | Panoramica (server, login, sito collegato) + elenco comandi |
| ` + "`quick status`" + ` | Stato del sito: visibilità reale, lock, e cosa salirebbe col deploy |
| ` + "`quick login`" + ` | Login Google (una volta; il token viene ricordato) |
| ` + "`quick deploy [<sito>] [cartella]`" + ` | Pubblica una cartella (default: quella corrente) |
| ` + "`quick ignore [cartella]`" + ` | Crea un ` + "`.quickignore`" + ` modificabile con i default già dentro |
| ` + "`quick publish <sito>`" + ` | Apri al pubblico (niente SSO) |
| ` + "`quick unpublish <sito>`" + ` | Torna dietro l'SSO aziendale (default) |
| ` + "`quick private <sito> [--code X]`" + ` | Accesso con codice (generato se assente) |
| ` + "`quick lock <sito>`" + ` / ` + "`quick unlock <sito>`" + ` | Blocca/sblocca la sovrascrittura (solo l'owner) |
| ` + "`quick delete <sito>`" + ` | Elimina il sito (irreversibile, con conferma) |

` + "`<sito>`" + ` è opzionale se nella cartella c'è un ` + "`.quick`" + `: in quel caso il nome
e il server vengono da lì. Senza ` + "`.quick`" + ` e senza nome, il sito prende il nome
della cartella corrente.

## Deploy: è un mirror

**Il deploy sostituisce l'intero contenuto del sito**, non aggiunge: i file non
presenti nel pacchetto vengono rimossi dal sito. Conseguenze:

- Per aggiornare un singolo file ripubblica comunque tutta la cartella.
- Un deploy da una cartella vuota azzererebbe il sito: la CLI lo **blocca**
  (serve ` + "`--force`" + ` per svuotare di proposito).
- Prima di pubblicare la CLI mostra un riepilogo (numero file, dimensione) e
  chiede conferma. Salta la conferma con ` + "`--yes`" + `; in contesti non interattivi
  senza ` + "`--yes`" + ` il deploy si rifiuta, per sicurezza.

Flag utili: ` + "`--dry-run`" + ` (mostra cosa salirebbe senza pubblicare),
` + "`--yes`" + `, ` + "`--force`" + `, ` + "`--public`" + ` / ` + "`--private[=codice]`" + ` (visibilità subito dopo il deploy).

## Cosa NON viene pubblicato

Le esclusioni si decidono in tre livelli:

1. **Sicurezza (sempre, non scavalcabile):** i file nascosti (` + "`.git`" + `, ` + "`.env`" + `, ` + "`.quick`" + `…,
   tranne ` + "`.well-known`" + `) e i segreti (` + "`*.pem`" + `, ` + "`*.key`" + `, ` + "`id_rsa`" + `, keystore).
2. **Comodità (predefinite, scavalcabili):** ` + "`node_modules/`" + `, ` + "`vendor/`" + `, ` + "`*.log`" + `, file temporanei.
3. **` + "`.quickignore`" + ` (se presente):** diventa la fonte di verità delle esclusioni di
   comodità (sintassi gitignore, con ` + "`!`" + ` per riammettere). Crealo con ` + "`quick ignore`" + `.

Usa ` + "`quick status`" + ` o ` + "`quick deploy ... --dry-run`" + ` per vedere file inclusi/esclusi.

## Convenzioni del sito servito

- ` + "`index.html`" + ` è l'indice di una cartella; ` + "`/about`" + ` serve ` + "`about.html`" + ` oppure
  ` + "`about/index.html`" + ` (URL puliti senza estensione).
- ` + "`404.html`" + ` in radice: pagina mostrata (con status 404) per i path inesistenti.
- ` + "`200.html`" + ` in radice: app shell per le SPA; viene servita (status 200) per
  qualsiasi rotta che non corrisponde a un file. Senza di essa i path mancanti
  danno un 404 vero (niente fallback silenzioso sulla home).

## Note

- Nuovi sottodomini sono immediati; i cambi di visibilità sono istantanei.
- Un sito **bloccato** può essere sovrascritto o eliminato solo dal suo owner.
`

func skillCmd(args []string) {
	fs := flag.NewFlagSet("skill", flag.ExitOnError)
	target := fs.String("target", "claude", "agente di destinazione (claude, codex, gemini, …)")
	project := fs.Bool("project", false, "scrivi in .<target>/skills/quick del progetto invece che globale")
	all := fs.Bool("all", false, "pubblica per tutti gli agenti noti (claude, codex, gemini)")
	dir := fs.String("dir", "", "cartella esplicita (ignora --target/--project)")
	fs.Parse(args)
	if *dir == "" && fs.NArg() > 0 {
		*dir = fs.Arg(0)
	}

	var dirs []string
	switch {
	case *dir != "":
		dirs = []string{*dir}
	case *all:
		for _, t := range []string{"claude", "codex", "gemini"} {
			dirs = append(dirs, skillDir(t, *project))
		}
	default:
		dirs = []string{skillDir(*target, *project)}
	}

	for _, d := range dirs {
		dst := filepath.Join(d, "SKILL.md")
		if err := os.MkdirAll(d, 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(dst, []byte(skillDoc), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("✓ skill pubblicata in %s\n", dst)
	}
	fmt.Println("  Formato SKILL.md aperto: lo leggono Claude Code, Codex, Gemini, Cursor e altri.")
}

// skillDir costruisce la cartella della skill secondo lo schema cross-vendor:
// ~/.<tool>/skills/quick (globale) o .<tool>/skills/quick (progetto).
func skillDir(tool string, project bool) string {
	if tool == "" || strings.ContainsAny(tool, "/\\.") {
		fatal(fmt.Errorf("target %q non valido", tool))
	}
	if project {
		return filepath.Join("."+tool, "skills", "quick")
	}
	home, err := os.UserHomeDir()
	fatal(err)
	return filepath.Join(home, "."+tool, "skills", "quick")
}
