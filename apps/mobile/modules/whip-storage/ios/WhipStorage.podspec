Pod::Spec.new do |s|
  s.name = 'WhipStorage'
  s.version = '0.1.0'
  s.summary = 'Whip private durable storage directory'
  s.description = 'Locates a backup-excluded directory for the encrypted Whip database.'
  s.license = { :type => 'Apache-2.0' }
  s.author = 'Context Labs'
  s.homepage = 'https://github.com/context-labs/whip'
  s.platforms = { :ios => '16.4' }
  s.source = { :git => 'https://github.com/context-labs/whip.git' }
  s.static_framework = true
  s.dependency 'ExpoModulesCore'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
  s.source_files = '**/*.{h,m,mm,swift}'
  s.swift_version = '5.9'
end
