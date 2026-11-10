# Licence AGPL du projet — conception

## Intention

Placer l'intégralité de la version actuelle de WoL Manager sous la GNU Affero General Public License version 3, strictement (`AGPL-3.0-only`), afin que les versions modifiées exploitées via un réseau offrent leur code source correspondant à leurs utilisateurs.

## Portée

- Remplacer la licence MIT du dépôt par le texte officiel intégral de l'AGPL v3.
- Présenter tout le projet actuel comme étant sous `AGPL-3.0-only`.
- Mettre à jour les deux mentions de licence du `README.md` et y résumer l'obligation réseau.
- Ne pas ajouter de restriction non commerciale.
- Ne pas modifier le comportement de l'application ni ajouter d'en-têtes à chaque fichier source.
- Ne pas documenter la transition depuis MIT dans le `README.md`.

## Approches considérées

- **MIT ou GPL v3 :** écartées, car elles ne répondent pas à l'exigence de restitution des modifications utilisées comme service réseau.
- **Licence non commerciale :** écartée, car elle empêcherait de présenter le projet comme open source.
- **AGPL v3 :** retenue, car son article 13 couvre précisément l'interaction à distance avec une version modifiée.

## Contraintes

- Utiliser l'identifiant SPDX `AGPL-3.0-only`, sans option « or later ».
- Reprendre le texte officiel sans modification.
- Préserver toutes les modifications locales existantes sans rapport avec la licence.

## Hors périmètre

- Réécriture de l'historique Git.
- Commit, push, release ou reconstruction des artefacts `dist/`.
- Avis juridique professionnel ou dispositif technique de contrôle des usages.
