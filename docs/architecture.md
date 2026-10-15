# Architecture

## Python 3 et Go

Python facilite le prototypage et dispose d'un large écosystème, mais une distribution autonome demande un interpréteur, un environnement virtuel ou un empaquetage supplémentaire. Go compile un exécutable autonome, fournit HTTP, TLS, JSON, UDP et les tests dans sa bibliothèque standard, et se distribue simplement pour Linux `amd64` et `arm64`.

WoL Manager utilise donc Go. La seule dépendance applicative directe est `gopkg.in/yaml.v3`, retenue pour une lecture YAML stricte. Il n'utilise ni framework Web, ni framework CLI, ni ORM.

## Flux

La CLI, le menu, l'API et les automatisations appellent le même moteur. Celui-ci résout les dépendances, revalide la politique UPS, sélectionne un transport autorisé, envoie le paquet puis exécute les vérifications configurées.

Les tâches sont enregistrées dans un fichier JSON remplacé atomiquement. Une tâche qui était active au redémarrage devient `revalidation_required`; un état enregistré ne déclenche jamais seul un paquet WoL.

## Paquets

Le code reste volontairement regroupé dans `cmd/wol` : une seule application, une seule implémentation par fonction. Les fichiers séparent configuration, réseau, vérification, NUT, jobs, API et CLI sans introduire de couches d'interface spéculatives.

## Automatisation

Le service observe les transitions NUT. L'état initial `OL` n'est pas un retour secteur. Seule une transition observée depuis un état différent vers `OL`, stable pendant `stable_for`, peut créer une tâche. La tâche revalide encore les politiques et dépendances.
