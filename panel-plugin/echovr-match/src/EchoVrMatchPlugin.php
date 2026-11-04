<?php

namespace MarshmallowMia\EchoVrMatch;

use Filament\Contracts\Plugin;
use Filament\Panel;

class EchoVrMatchPlugin implements Plugin
{
    public function getId(): string
    {
        return 'echovr-match';
    }

    public function register(Panel $panel): void
    {
        $id = str($panel->getId())->title();

        $panel->discoverPages(plugin_path($this->getId(), "src/Filament/$id/Pages"), "MarshmallowMia\\EchoVrMatch\\Filament\\$id\\Pages");
        $panel->discoverWidgets(plugin_path($this->getId(), "src/Filament/$id/Widgets"), "MarshmallowMia\\EchoVrMatch\\Filament\\$id\\Widgets");
    }

    public function boot(Panel $panel): void {}
}
