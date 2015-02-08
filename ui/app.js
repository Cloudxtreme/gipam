var ipam = angular.module('ipam',['ui.bootstrap']);

ipam.controller('IPAM', function($scope, $http) {
    $http.get('/api/list').success(function(data) {
        $scope.allocs = data.Allocs;
    });
});
ipam.directive('ipblocks', function() {
    return {
        restrict: 'E',
        scope: {
            allocs: '='
        },
        templateUrl: "alloc.html"
    };
});
