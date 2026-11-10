# AGPL Licensing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Relicencier toute la version actuelle de WoL Manager sous `AGPL-3.0-only`.

**Architecture:** Le changement est documentaire et limité au fichier de licence canonique et aux mentions publiques du README. Aucun comportement exécutable, artefact de distribution ou historique Git n'est modifié.

**Tech Stack:** Markdown, texte officiel GNU AGPL v3, Git.

**Spec:** `docs/superpowers/specs/2026-11-10-agpl-licensing-design.md`

## Global Constraints

- Utiliser exactement `AGPL-3.0-only`, sans clause « or later ».
- Copier le texte officiel complet de la GNU AGPL v3 sans le modifier.
- Préserver toutes les modifications locales sans rapport avec la licence.
- Ne pas effectuer de commit, push, release ou reconstruction de `dist/`.

## Review Focus

- Le fichier `LICENSE` doit contenir le texte AGPL v3 complet et ne plus contenir le texte MIT.
- Aucune mention « Licence MIT » ne doit subsister dans les fichiers textuels du projet hors historique Git et artefacts existants.
- Le résumé du README ne doit pas prétendre interdire les usages commerciaux.
- Le résumé doit viser les versions modifiées accessibles via un réseau, conformément à l'article 13.
- Le diff ne doit pas absorber ou altérer les autres changements locaux en cours.

---

### Task 1: Remplacer la licence canonique

**Files:**
- Modify: `LICENSE`

**Interfaces:**
- Consumes: texte officiel GNU AGPL v3.
- Produces: fichier de licence canonique du dépôt sous `AGPL-3.0-only`.

- [x] **Step 1: Remplacer le contenu de `LICENSE`**

Reprendre sans modification le texte officiel complet disponible à `https://www.gnu.org/licenses/agpl-3.0.txt`.

- [x] **Step 2: Vérifier le texte**

Run: comparer le SHA-256 de `LICENSE` avec une copie fraîche du texte officiel et rechercher `GNU AFFERO GENERAL PUBLIC LICENSE` ainsi que `Version 3`.

Expected: empreintes identiques et les deux mentions présentes.

### Task 2: Mettre à jour la présentation publique

**Files:**
- Modify: `README.md:13`
- Modify: `README.md:254`

**Interfaces:**
- Consumes: licence canonique créée à la tâche 1.
- Produces: badge et section de licence cohérents avec `AGPL-3.0-only`.

- [x] **Step 1: Renommer le badge**

Remplacer le libellé `Licence MIT` par `Licence AGPL v3` en conservant le lien vers `LICENSE`.

- [x] **Step 2: Remplacer la phrase finale de licence**

Indiquer que tout le projet est sous `AGPL-3.0-only`, que redistribution et modification restent sous cette licence et qu'une version modifiée proposée via un réseau doit offrir son code source correspondant aux utilisateurs distants.

- [x] **Step 3: Vérifier les références**

Run: `rg -n -i --glob '!dist/**' --glob '!docs/images/*.svg' --glob '!docs/diagrams/*.drawio' "MIT|AGPL|licen[cs]e" LICENSE README.md`

Expected: aucune ancienne mention MIT ; mentions AGPL cohérentes dans `LICENSE` et `README.md`.

### Task 3: Contrôler le diff final

**Files:**
- Verify: `LICENSE`
- Verify: `README.md`
- Verify: `docs/superpowers/specs/2026-11-10-agpl-licensing-design.md`
- Verify: `docs/superpowers/plans/2026-11-10-agpl-licensing.md`

**Interfaces:**
- Consumes: résultats des tâches 1 et 2.
- Produces: preuve que la relicence est complète et isolée.

- [x] **Step 1: Contrôler la propreté textuelle**

Run: `git diff --check`

Expected: aucune erreur.

- [x] **Step 2: Examiner uniquement le diff de licence**

Run: `git diff -- LICENSE README.md docs/superpowers/specs/2026-11-10-agpl-licensing-design.md docs/superpowers/plans/2026-11-10-agpl-licensing.md`

Expected: seuls le remplacement de licence, les deux changements du README et les documents de décision/planification apparaissent dans ce périmètre.

- [x] **Step 3: Vérifier l'état sans publier**

Run: `git status --short`

Expected: les fichiers de licence sont modifiés localement ; aucun commit, push, release ou artefact `dist/` n'a été produit par ce plan.
