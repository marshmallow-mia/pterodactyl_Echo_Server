<?php

return [
    // The Echo VR (nevr-runtime) egg that evr-ptero imports; servers of other
    // eggs are left alone. An egg tagged "echovr" counts too.
    'egg_uuid' => '6a3c1e5e-8f4b-4d2a-9c71-3e0f5b7d2a10',
    'egg_tag' => 'echovr',

    // Seconds the panel keeps a server's match files before asking Wings again.
    'cache_seconds' => 2,

    // How often the views refresh.
    'poll' => [
        'widget' => '3s',
        'page' => '3s',
        'admin' => '5s',
    ],
];
