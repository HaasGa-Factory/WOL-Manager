# Politique de sécurité

La branche Beta reçoit les correctifs de sécurité jusqu'à la publication d'une Beta suivante ou d'une version stable.

Ne publiez pas une vulnérabilité exploitable dans une issue publique. Contactez le mainteneur par le canal privé de sécurité du futur dépôt GitHub en indiquant version, impact, reproduction minimale et correctif suggéré. Aucun secret ou détail d'infrastructure réel ne doit être inclus.

Les broadcasts dirigés, diagnostics actifs et accès distants sont sensibles. Une configuration qui les active constitue une décision administrative explicite, pas un comportement par défaut.

Les mises à jour sont téléchargées uniquement en HTTPS depuis `HaasGa-Factory/WOL-Manager`, après résolution du canal vers un commit immuable. Un manifeste signé Ed25519 fournit les SHA-256, l'architecture, le canal et la révision source attendus. La commande `wol update` n'exécute aucun script distant : elle délègue à l'installateur local appartenant à root et non modifiable par le groupe ou les autres utilisateurs.

La clé privée de distribution n'est jamais versionnée. La clé publique `wol-manager-2026-01` est intégrée à l'installation. Une rotation doit d'abord distribuer la nouvelle clé publique avec une version signée par l'ancienne clé ; le passage exclusif à la nouvelle clé intervient seulement après cette transition. La perte ou la compromission de la clé privée exige une intervention manuelle et ne doit jamais être contournée par un SHA-256 non signé.
