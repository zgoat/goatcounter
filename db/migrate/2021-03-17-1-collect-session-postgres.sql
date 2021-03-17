update sites set settings = jsonb_set(settings, '{collect}',
	to_jsonb(coalesce(cast(settings->'collect' as int) + 128, 62)), true);
