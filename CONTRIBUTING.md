# Contribution

1. Ouvrir une issue décrivant le besoin et les implications réseau/sécurité.
2. Garder les changements minimaux, compatibles avec le binaire unique et sans dépendance spéculative.
3. Ajouter ou adapter un test sans serveur physique, VLAN ou UPS réel.
4. Exécuter `gofmt`, `go test ./...` et `go vet ./...`.
5. Ne jamais joindre de configuration privée, adresse d'exploitation, jeton ou clé.

Les changements de protocole, de sécurité ou de format persistant doivent être documentés.
