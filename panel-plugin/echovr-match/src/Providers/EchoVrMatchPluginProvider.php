<?php

namespace MarshmallowMia\EchoVrMatch\Providers;

use App\Enums\ConsoleWidgetPosition;
use App\Filament\Server\Pages\Console;
use Illuminate\Support\ServiceProvider;
use MarshmallowMia\EchoVrMatch\Filament\Server\Widgets\MatchWidget;

class EchoVrMatchPluginProvider extends ServiceProvider
{
    public function register(): void
    {
        Console::registerCustomWidgets(ConsoleWidgetPosition::AboveConsole, [MatchWidget::class]);
    }

    public function boot(): void {}
}
