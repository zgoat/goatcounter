update sites set settings = json_set(settings, '$.collect',
	coalesce(json_extract('$.collect') + 128, 63));
