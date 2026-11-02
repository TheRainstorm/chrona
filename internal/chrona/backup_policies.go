package chrona

// apply resolves time settings without changing machine, datasets, pause state or job identity.
func (p BackupPolicy) apply(b Backup) Backup {
	b.Cadence, b.Weekdays, b.MonthDay, b.At, b.GraceMinutes = p.Cadence, append([]int{}, p.Weekdays...), p.MonthDay, p.At, p.GraceMinutes
	return b
}

func (c Config) ResolveBackup(b Backup) Backup {
	if b.Policy != "" {
		for _, p := range c.BackupPolicies {
			if p.ID == b.Policy {
				b = p.apply(b)
				b.PolicyName = p.Name
				break
			}
		}
	}
	return b
}

// Only missing backup_policies identifies the original combined format. An explicit
// empty collection permits new custom jobs without creating public definitions.
func migrateLegacyBackupPolicies(c *Config) {
	for i := range c.Backups {
		b := &c.Backups[i]
		if b.Private || b.Policy != "" {
			continue
		}
		id := b.ID
		// Duplicate legacy job IDs remain invalid rather than silently changing identity.
		c.BackupPolicies = append(c.BackupPolicies, BackupPolicy{ID: id, Name: b.Name, Cadence: b.Cadence, Weekdays: append([]int{}, b.Weekdays...), MonthDay: b.MonthDay, At: b.At, GraceMinutes: b.GraceMinutes})
		b.Policy = id
		b.Cadence, b.Weekdays, b.MonthDay, b.At, b.GraceMinutes = "", nil, 0, "", 0
	}
}
