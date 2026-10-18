# Architecture

## Python 3 et Go

Python facilite le prototypage et dispose d'un large écosystème, mais une distribution autonome demande un interpréteur, un environnement virtuel ou un empaquetage supplémentaire. Go compile un exécutable autonome, fournit HTTP, TLS, JSON, UDP et les tests dans sa bibliothèque standard, et se distribue simplement pour Linux `amd64` et `arm64`.

WoL Manager utilise donc Go. La seule dépendance applicative directe est `gopkg.in/yaml.v3`, retenue pour une lecture YAML stricte. Il n'utilise ni framework Web, ni framework CLI, ni ORM.

## Flux

La CLI, le menu, l'API et les automatisations appellent le même moteur. Celui-ci résout les dépendances, revalide la politique UPS, sélectionne un transport autorisé, envoie le paquet puis exécute les vérifications configurées.

Les tâches et cycles électriques observés sont enregistrés dans un fichier JSON remplacé atomiquement. Une tâche qui était active au redémarrage devient `revalidation_required`; un état enregistré ne déclenche jamais seul un paquet WoL.

## Paquets

Le code reste volontairement regroupé dans `cmd/wol` : une seule application, une seule implémentation par fonction. Les fichiers séparent configuration, réseau, vérification, NUT, jobs, API et CLI sans introduire de couches d'interface spéculatives.

## Automatisation

Le service observe les états NUT par UPS. L'état initial `OL` n'est pas un retour secteur. Un cycle doit d'abord être enregistré pendant `OB`, `LB` ou `FSD`, puis réarmé au seuil bas. Le secteur `OL` doit rester stable pendant `stable_for` et la batterie atteindre le seuil haut. Une valeur absente ou invalide bloque la décision au lieu d'être interprétée comme zéro.

Les groupes éligibles sont ordonnés par priorité puis identifiant. Le mode séquentiel respecte l'ordre configuré ; le mode parallèle conserve les dépendances obligatoires. Un coordinateur partagé évite les doublons lorsqu'une machine appartient à plusieurs groupes. Le verrou de puissance est revérifié immédiatement avant chaque émission.

Une nouvelle coupure bloque l'UPS concerné et annule les tâches automatiques actives. Après redémarrage, les cycles incomplets repassent en attente et doivent reconstruire leur durée de stabilité avant toute reprise.
